package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastPolling(t *testing.T) {
	t.Helper()
	cloudflare, solverQ := cloudflareSolverPollInterval, solverQPollInterval
	cloudflareSolverPollInterval, solverQPollInterval = time.Millisecond, time.Millisecond
	t.Cleanup(func() { cloudflareSolverPollInterval, solverQPollInterval = cloudflare, solverQ })
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func TestCloudflareSolverPollsUntilReady(t *testing.T) {
	fastPolling(t)
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer sk-cf" {
			t.Errorf("unexpected request: %s %q", r.Method, r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/turnstile":
			if r.URL.Query().Get("url") != "https://site.example/" || r.URL.Query().Get("sitekey") != "0x4AAA" {
				t.Errorf("unexpected task query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, `{"errorId":0,"taskId":"task-1"}`)
		case "/result":
			if r.URL.Query().Get("id") != "task-1" {
				t.Errorf("unexpected task id: %s", r.URL.RawQuery)
			}
			if polls.Add(1) < 2 {
				writeJSON(w, http.StatusOK, `{"status":"processing"}`)
				return
			}
			writeJSON(w, http.StatusOK, `{"errorId":0,"status":"ready","solution":{"token":"0.cf-token"},"elapsed_time":1.5}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	token, err := Solve(context.Background(), server.Client(), []Solver{{Platform: PlatformCloudflareSolver, APIKey: " sk-cf ", BaseURL: server.URL}}, "https://site.example/", "0x4AAA")
	if err != nil || token != "0.cf-token" || polls.Load() != 2 {
		t.Fatalf("token=%q polls=%d err=%v", token, polls.Load(), err)
	}
}

func TestSolverQSendsKeyInBody(t *testing.T) {
	fastPolling(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientKey string            `json:"clientKey"`
			TaskID    string            `json:"taskId"`
			Task      map[string]string `json:"task"`
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || json.NewDecoder(r.Body).Decode(&body) != nil || body.ClientKey != "sk-q" {
			t.Errorf("unexpected request: %s %+v", r.Method, body)
		}
		switch r.URL.Path {
		case "/createTask":
			if body.Task["type"] != "TurnstileTaskProxyless" || body.Task["websiteURL"] != "https://site.example/" || body.Task["websiteKey"] != "0x4AAA" {
				t.Errorf("unexpected task: %+v", body.Task)
			}
			writeJSON(w, http.StatusOK, `{"errorId":0,"errorCode":"","errorDescription":"","taskId":"task-q"}`)
		case "/getTaskResult":
			if body.TaskID != "task-q" {
				t.Errorf("unexpected task id: %q", body.TaskID)
			}
			writeJSON(w, http.StatusOK, `{"errorId":0,"status":"ready","solution":{"token":"0.q-token","userAgent":"Mozilla/5.0"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	token, err := Solve(context.Background(), server.Client(), []Solver{{Platform: PlatformSolverQ, APIKey: "sk-q", BaseURL: server.URL}}, "https://site.example/", "0x4AAA")
	if err != nil || token != "0.q-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestSolveFallsBackInOrder(t *testing.T) {
	fastPolling(t)
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, `{"errorId":1,"errorCode":"ERROR_NO_SLOT_AVAILABLE","errorDescription":"busy"}`)
	}))
	defer busy.Close()
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createTask" {
			writeJSON(w, http.StatusOK, `{"errorId":0,"taskId":"task-q"}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"errorId":0,"status":"ready","solution":{"token":"0.fallback"}}`)
	}))
	defer ready.Close()
	token, err := Solve(context.Background(), nil, []Solver{
		{Platform: PlatformCloudflareSolver, APIKey: "sk-cf", BaseURL: busy.URL},
		{Platform: PlatformSolverQ, APIKey: "sk-q", BaseURL: ready.URL},
	}, "https://site.example/", "0x4AAA")
	if err != nil || token != "0.fallback" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestSolveReportsEveryPlatformWithoutSecrets(t *testing.T) {
	fastPolling(t)
	unsolvable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/turnstile" {
			writeJSON(w, http.StatusOK, `{"errorId":0,"taskId":"task-1"}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"errorId":1,"errorCode":"ERROR_CAPTCHA_UNSOLVABLE","errorDescription":"third-party text sk-cf"}`)
	}))
	defer unsolvable.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer broken.Close()
	_, err := Solve(context.Background(), nil, []Solver{
		{Platform: PlatformCloudflareSolver, APIKey: "sk-cf", BaseURL: unsolvable.URL},
		{Platform: PlatformSolverQ, APIKey: "sk-q", BaseURL: broken.URL},
	}, "https://site.example/", "0x4AAA")
	want := "Cloudflare Solver: ERROR_CAPTCHA_UNSOLVABLE; SolverQ: HTTP status 502"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestSolveDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		writeJSON(w, http.StatusOK, `{"errorId":0,"taskId":"stolen"}`)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	_, err := Solve(context.Background(), &http.Client{}, []Solver{{Platform: PlatformSolverQ, APIKey: "sk-q", BaseURL: origin.URL}}, "https://site.example/", "0x4AAA")
	if err == nil || redirected.Load() || !strings.Contains(err.Error(), "HTTP status 307") {
		t.Fatalf("redirected=%v err=%v", redirected.Load(), err)
	}
}

func TestSolveReturnsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		writeJSON(w, http.StatusOK, `{"errorId":0,"taskId":"task-1"}`)
	}))
	defer server.Close()
	_, err := Solve(ctx, nil, []Solver{
		{Platform: PlatformCloudflareSolver, APIKey: "sk-cf", BaseURL: server.URL},
		{Platform: PlatformSolverQ, APIKey: "sk-q", BaseURL: server.URL},
	}, "https://site.example/", "0x4AAA")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
