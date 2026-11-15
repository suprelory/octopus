package turnstile

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskProtocols(t *testing.T) {
	for _, platform := range []string{CloudflareSolver, SolverQ} {
		t.Run(platform, func(t *testing.T) {
			var creates, polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "" || r.Header.Get("New-Api-User") != "" || r.Header.Get("Proxy-Authorization") != "" {
					t.Error("site identity reached solver")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/turnstile":
					creates.Add(1)
					if platform != CloudflareSolver || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer solver-private-key" || r.URL.Query().Get("url") != "https://site.example/prefix/login" || r.URL.Query().Get("sitekey") != "0x-test" || r.URL.Query().Get("action") != "login" || r.URL.Query().Get("cdata") != "widget-data" {
						t.Error("invalid Cloudflare Solver task")
					}
					_, _ = io.WriteString(w, `{"errorId":0,"taskId":"task-one"}`)
				case "/api/createTask":
					creates.Add(1)
					var body struct {
						ClientKey string
						Task      struct {
							Type, WebsiteURL, WebsiteKey string
							Metadata                     map[string]string
						}
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || platform != SolverQ || r.Method != "POST" || r.Header.Get("Authorization") != "" || body.ClientKey != "solver-private-key" || body.Task.Type != "TurnstileTaskProxyless" || body.Task.WebsiteURL != "https://site.example/prefix/login" || body.Task.WebsiteKey != "0x-test" || body.Task.Metadata["action"] != "login" || body.Task.Metadata["cdata"] != "widget-data" {
						t.Error("invalid SolverQ task")
					}
					_, _ = io.WriteString(w, `{"errorId":0,"taskId":"task-one"}`)
				case "/api/result", "/api/getTaskResult":
					if platform == CloudflareSolver {
						if r.URL.Query().Get("id") != "task-one" || r.Method != "GET" {
							t.Error("invalid GET poll")
						}
					} else {
						var body map[string]string
						_ = json.NewDecoder(r.Body).Decode(&body)
						if body["clientKey"] != "solver-private-key" || body["taskId"] != "task-one" || r.Method != "POST" {
							t.Error("invalid POST poll")
						}
					}
					if polls.Add(1) == 1 {
						if platform == CloudflareSolver {
							_, _ = io.WriteString(w, `{"status":"processing"}`)
						} else {
							_, _ = io.WriteString(w, `{"errorId":0,"status":"processing"}`)
						}
					} else {
						_, _ = io.WriteString(w, `{"errorId":0,"status":"ready","solution":{"token":"private-result-token"}}`)
					}
				default:
					t.Error("unexpected solver request", r.URL.Path)
				}
			}))
			defer server.Close()
			token, err := solve(context.Background(), server.Client(), Solver{Platform: platform, APIKey: "solver-private-key", BaseURL: server.URL + "/api"}, Task{PageURL: "https://site.example/prefix/login", SiteKey: "0x-test", Action: "login", CData: "widget-data"}, time.Millisecond)
			if err != nil || token != "private-result-token" || creates.Load() != 1 || polls.Load() != 2 {
				t.Fatalf("protocol failed: %q %v", token, err)
			}
		})
	}
}

func TestSolverRejectsMalformedResponsesAndRedactsErrors(t *testing.T) {
	for _, test := range []struct {
		name, body string
		poll       bool
		want       error
	}{
		{"missing error ID", `{"taskId":"x"}`, false, ErrProtocol},
		{"null error ID", `{"errorId":null,"taskId":"x"}`, false, ErrProtocol},
		{"empty task", `{"errorId":0}`, false, ErrProtocol},
		{"HTML", `<html>private-key</html>`, false, ErrProtocol},
		{"oversized", strings.Repeat("x", (64<<10)+1), false, ErrProtocol},
		{"untrusted code", `{"errorId":1,"errorCode":"private-key-and-token","errorDescription":"private-cookie"}`, false, ErrProtocol},
		{"balance", `{"errorId":1,"errorCode":"ERROR_ZERO_BALANCE","errorDescription":"private-cookie"}`, false, ErrBalance},
		{"key", `{"errorId":1,"errorCode":"ERROR_KEY_DOES_NOT_EXIST"}`, false, ErrKey},
		{"busy", `{"errorCode":"ERROR_SERVICE_UNAVALIABLE"}`, false, ErrBusy},
		{"empty result", `{"errorId":0,"status":"ready","solution":{"token":""}}`, true, ErrProtocol},
		{"missing result error ID", `{"status":"ready","solution":{"token":"x"}}`, true, ErrProtocol},
		{"token controls", `{"errorId":0,"status":"ready","solution":{"token":"x\r\ny"}}`, true, ErrProtocol},
		{"unsolvable", `{"errorId":1,"errorCode":"ERROR_CAPTCHA_UNSOLVABLE"}`, true, ErrUnsolvable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := test.body
				if test.poll && r.URL.Path == "/turnstile" {
					body = `{"errorId":0,"taskId":"one"}`
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			token, err := solve(context.Background(), server.Client(), Solver{Platform: CloudflareSolver, APIKey: "private-key", BaseURL: server.URL}, Task{PageURL: "https://site.example", SiteKey: "0x-test"}, time.Millisecond)
			if err != test.want || token != "" || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe or unexpected solver result: %q %v", token, err)
			}
		})
	}
}

func TestSolverCancellationAndRedirectsNeverCreateAnotherTask(t *testing.T) {
	for _, mode := range []string{"processing", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var calls, followed atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/leak" {
					followed.Add(1)
					return
				}
				if r.URL.Path == "/turnstile" {
					calls.Add(1)
					if mode == "redirect" {
						http.Redirect(w, r, "/leak", 307)
						return
					}
					_, _ = io.WriteString(w, `{"errorId":0,"taskId":"one"}`)
				} else {
					_, _ = io.WriteString(w, `{"status":"processing"}`)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
			defer cancel()
			_, err := solve(ctx, server.Client(), Solver{Platform: CloudflareSolver, APIKey: "key", BaseURL: server.URL}, Task{PageURL: "https://site.example", SiteKey: "0x-test"}, time.Millisecond)
			if err == nil || (mode == "processing" && err != ErrTimeout) || calls.Load() != 1 || followed.Load() != 0 {
				t.Fatal("task was replayed or did not stop", err)
			}
		})
	}
}
