package checkin

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestClassifyNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"DNS", &net.DNSError{Err: "no such host", Name: "private.example"}, model.CheckinNetworkDNSError},
		{"proxy", &net.OpError{Op: "proxyconnect", Net: "tcp", Err: syscall.ECONNREFUSED}, model.CheckinNetworkProxyError},
		{"TLS", tls.RecordHeaderError{Msg: "remote error"}, model.CheckinNetworkTLSError},
		{"timeout", &url.Error{Op: "Get", URL: "https://private.example", Err: context.DeadlineExceeded}, model.CheckinNetworkTimeout},
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, model.CheckinNetworkConnectionRefused},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, model.CheckinNetworkConnectionReset},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyNetworkError(test.err); got != test.want {
				t.Fatalf("classified as %q, want %q", got, test.want)
			}
		})
	}
}

func TestDoRequestHTTPClassifiesOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
	}))
	defer server.Close()
	result := doRequestHTTP(context.Background(), server.Client(), model.CheckinSiteConfig{BaseURL: server.URL, Adapter: "http"}, model.CheckinRequest{Method: "GET", Path: "/"}, model.CheckinCredentials{}, "2030-11-14", false)
	if result.StatusCode != http.StatusOK || result.Err == nil || result.NetworkReason != model.CheckinNetworkResponseTooLarge {
		t.Fatalf("oversized response was not classified: %+v", result)
	}
}

func TestAnyRouterShieldChallengeLoginAndTokenFormats(t *testing.T) {
	h := newHarness(t)
	const shieldCookie = "challenge-seed"
	claims, _ := json.Marshal(map[string]int{"sub": 42})
	loginToken := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	challenge := makeAnyRouterChallengeFixture()
	if value := anyRouterSolveAcwScV2(challenge); value == "" {
		t.Fatal("test shield challenge fixture could not be solved")
	}
	var loginRequests, selfRequests, checkinRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != anyRouterUserAgent || r.Header.Get("Accept") != "application/json, text/plain, */*" || r.Header.Get("Accept-Language") != "zh-CN,zh;q=0.9,en;q=0.8" || r.Header.Get("Origin") != server.URL || r.Header.Get("Referer") != server.URL+"/console" {
			t.Errorf("browser headers missing or incorrect: %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/user/login":
			loginRequests++
			if !strings.Contains(r.Header.Get("Cookie"), "acw_sc__v2=") {
				http.SetCookie(w, &http.Cookie{Name: "cdn_sec_tc", Value: shieldCookie, Path: "/"})
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(challenge))
				return
			}
			if !strings.Contains(r.Header.Get("Cookie"), "cdn_sec_tc="+shieldCookie) {
				t.Errorf("challenge cookie missing on retry: %q", r.Header.Get("Cookie"))
			}
			jsonReply(w, `{"success":true,"data":"`+loginToken+`"}`)
		case "/api/user/self":
			selfRequests++
			if r.Header.Get("Authorization") != "Bearer "+loginToken || r.Header.Get("New-Api-User") != "42" || r.Header.Get("Veloera-User") != "42" || r.Header.Get("voapi-user") != "42" || r.Header.Get("User-id") != "42" || r.Header.Get("Rix-Api-User") != "42" || r.Header.Get("neo-api-user") != "42" || !strings.Contains(r.Header.Get("Cookie"), "acw_sc__v2=") {
				t.Errorf("verified login credentials or shield cookies missing: auth=%q cookie=%q", r.Header.Get("Authorization"), r.Header.Get("Cookie"))
			}
			jsonReply(w, `{"success":true,"data":{"id":42}}`)
		case "/api/user/checkin":
			checkinRequests++
			requireSubmitMarker(t, h)
			if r.Header.Get("Authorization") != "Bearer "+loginToken || r.Header.Get("New-Api-User") != "42" || r.Header.Get("Veloera-User") != "42" || r.Header.Get("voapi-user") != "42" || r.Header.Get("User-id") != "42" || r.Header.Get("Rix-Api-User") != "42" || r.Header.Get("neo-api-user") != "42" || !strings.Contains(r.Header.Get("Cookie"), "acw_sc__v2=") {
				t.Errorf("check-in request lost its verified session: auth=%q id=%q cookie=%q", r.Header.Get("Authorization"), r.Header.Get("New-Api-User"), r.Header.Get("Cookie"))
			}
			jsonReply(w, `{"ret":1}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := model.CheckinSiteConfig{Name: "AnyRouter", BaseURL: server.URL, Adapter: "anyrouter", Enabled: true, Timezone: "UTC"}
	site, err := h.s.Store.SaveSite(context.Background(), "", 0, config)
	if err != nil {
		t.Fatal(err)
	}
	name := "shielded account"
	account, err := h.s.SaveAccount(context.Background(), site.ID, "", AccountWrite{Name: &name, Credentials: &model.CheckinCredentials{Type: "password", Username: "user", Password: "password"}})
	if err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	if run := h.run(account); run.Status != "success" {
		attempts, _ := h.s.Store.Attempts(context.Background(), run.ID)
		t.Fatalf("AnyRouter login/check-in failed: run=%+v requests=(%d,%d,%d) attempts=%+v", run, loginRequests, selfRequests, checkinRequests, attempts)
	}
	if loginRequests != 2 || selfRequests != 1 || checkinRequests != 1 {
		t.Fatalf("unexpected requests: login=%d self=%d checkin=%d", loginRequests, selfRequests, checkinRequests)
	}
}

func TestAnyRouterAcceptsAccessTokenLoginFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"nested accessToken", `{"success":true,"data":{"id":42,"accessToken":"login-token"}}`},
		{"top-level accessToken", `{"success":true,"data":{"id":42},"accessToken":"login-token"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/user/login":
					jsonReply(w, test.body)
				case "/api/user/checkin":
					if r.Header.Get("Authorization") != "Bearer login-token" {
						t.Errorf("accessToken field not used: %q", r.Header.Get("Authorization"))
					}
					jsonReply(w, `{"ret":1}`)
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()
			_, account := h.platformAccount(server.URL, "anyrouter", model.CheckinCredentials{Type: "password", Username: "user", Password: "password"})
			h.enqueue(account, "manual")
			h.drain()
			if run := h.run(account); run.Status != "success" {
				t.Fatalf("login token field was not accepted: %+v", run)
			}
		})
	}
}

func TestCheckinAttemptRecordsSanitizedNetworkReason(t *testing.T) {
	h := newHarness(t)
	server := httptest.NewServer(http.NotFoundHandler())
	baseURL := server.URL
	server.Close()
	_, account := h.platformAccount(baseURL, "anyrouter", model.CheckinCredentials{Type: "password", Username: "private-user", Password: "private-password"})
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if run.Status != "manual_required" || run.Reason != model.CheckinNetworkError {
		t.Fatalf("unexpected run result: %+v", run)
	}
	attempts, err := h.s.Store.Attempts(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || !model.IsCheckinNetworkReason(attempts[0].Reason) || attempts[0].Reason == model.CheckinNetworkError {
		t.Fatalf("network failure was not categorized: %+v", attempts)
	}
	encoded, _ := json.Marshal(attempts[0])
	if strings.Contains(string(encoded), "private-user") || strings.Contains(string(encoded), "private-password") || strings.Contains(string(encoded), baseURL) {
		t.Fatalf("network details leaked into the attempt: %s", encoded)
	}
}

func makeAnyRouterChallengeFixture() string {
	const arg1 = "00112233445566778899aabbccddeeff00112233"
	const seed = "0123456789abcdef0123456789abcdef01234567"
	values := make([]string, 40)
	for index := range values {
		values[index] = obfuscChallengeBase64("1")
	}
	values[0x112-0xfb] = obfuscChallengeBase64("100000")
	values[0x115-0xfb] = obfuscChallengeBase64(seed)
	mapping := make([]string, 40)
	for index := range mapping {
		mapping[index] = fmt.Sprint(index + 1)
	}
	return fmt.Sprintf(`<html><script>var arg1='%s';for(var m=[%s],p=L(0x115);var N=['%s'];a0i=;}(a0i,11111),d=d-0xfb;</script></html>`, arg1, strings.Join(mapping, ","), strings.Join(values, "','"))
}

func obfuscChallengeBase64(value string) string {
	const obfuscatedAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/="
	const standardAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	var builder strings.Builder
	for _, char := range encoded {
		index := strings.IndexRune(standardAlphabet, char)
		builder.WriteByte(obfuscatedAlphabet[index])
	}
	return builder.String()
}
