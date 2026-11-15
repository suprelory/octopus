package checkin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func (h *harness) platformAccount(base, platform string, credentials model.CheckinCredentials) (model.CheckinSite, model.CheckinAccount) {
	h.t.Helper()
	config := model.CheckinSiteConfig{Name: platform, BaseURL: base, Adapter: platform, Enabled: true, Timezone: "UTC", UserAgent: "Checkin-Adapter-Test/1.0"}
	if platform == "sub2api" || platform == "http" {
		config.Submit.Path = "/checkin"
	}
	site, err := h.s.Store.SaveSite(context.Background(), "", 0, config)
	if err != nil {
		h.t.Fatal(err)
	}
	name := "Dedicated account"
	account, err := h.s.SaveAccount(context.Background(), site.ID, "", AccountWrite{Name: &name, Credentials: &credentials})
	if err != nil {
		h.t.Fatal(err)
	}
	return site, account
}

func requireSubmitMarker(t *testing.T, h *harness) {
	t.Helper()
	var count int64
	if err := h.s.Store.DB.Model(&model.CheckinRun{}).Where("submit_intent_at IS NOT NULL AND status = ?", "running").Count(&count).Error; err != nil || count != 1 {
		t.Error("platform mutation preceded the durable submit marker", err)
	}
}

func storedCredentials(t *testing.T, h *harness, account model.CheckinAccount) (model.CheckinAccount, model.CheckinCredentials) {
	t.Helper()
	latest, err := h.s.Store.Account(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := h.s.Vault.Unseal(account.ID, latest.CredentialCipher)
	if err != nil {
		t.Fatal(err)
	}
	var credentials model.CheckinCredentials
	if err := json.Unmarshal(plain, &credentials); err != nil {
		t.Fatal(err)
	}
	return latest, credentials
}

func TestPlatformRegistryAndCredentials(t *testing.T) {
	h := newHarness(t)
	for _, capability := range Capabilities() {
		t.Run(capability.ID, func(t *testing.T) {
			config := model.CheckinSiteConfig{Name: "registry", BaseURL: "https://example.invalid", Adapter: capability.ID, Submit: model.CheckinRequest{Path: capability.Path}}
			if config.Submit.Path == "" {
				config.Submit.Path = "/custom"
			}
			if err := model.NormalizeCheckinSite(&config); err != nil {
				t.Fatal(err)
			}
			if _, err := newPlatformAdapter(&adapterRuntime{site: model.CheckinSite{Config: config}}); err != nil {
				t.Fatal(err)
			}
			for _, credentials := range []model.CheckinCredentials{{Type: "cookie", Cookie: "session=x"}, {Type: "bearer", AccessToken: "token"}, {Type: "password", Username: "u", Password: "p"}} {
				site, err := h.s.Store.SaveSite(context.Background(), "", 0, config)
				if err != nil {
					t.Fatal(err)
				}
				name := "account"
				_, err = h.s.SaveAccount(context.Background(), site.ID, "", AccountWrite{Name: &name, Credentials: &credentials})
				if (err == nil) != model.CheckinCredentialSupported(capability.ID, credentials.Type) {
					t.Fatalf("credential capability disagrees for %s: %v", credentials.Type, err)
				}
			}
		})
	}
}

func TestNewAPINativeRecordsAndReward(t *testing.T) {
	h := newHarness(t)
	var submitted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/checkin" || r.Header.Get("Authorization") != "Bearer website-token" {
			t.Error("incorrect native request")
		}
		if r.Method == "POST" {
			requireSubmitMarker(t, h)
			submitted.Add(1)
			jsonReply(w, `{"success":true,"data":{"checkin_date":"2030-11-14","quota_awarded":12500}}`)
			return
		}
		if r.URL.Query().Get("month") != "2030-11" {
			t.Error("target month was not queried")
		}
		if submitted.Load() == 0 {
			jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[]}}}`)
		} else {
			jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":true,"records":[{"checkin_date":"2030-11-13","quota_awarded":90000},{"checkin_date":"2030-11-14","quota_awarded":12500}]}}}`)
		}
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "bearer", AccessToken: "website-token"})
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if run.Status != "success" || run.RewardMicros == nil || *run.RewardMicros != 12500*1_000_000 || run.RewardUnit != "quota" {
		t.Fatalf("wrong native result: %+v", run)
	}
	h.enqueue(account, "manual")
	h.drain()
	if submitted.Load() != 1 {
		t.Fatal("daily native check-in replayed")
	}
}

func TestNewAPICrossMonthConfirmationAndExplicitStatusDisable(t *testing.T) {
	h := newHarness(t)
	h.now = time.Date(2030, 11, 30, 23, 59, 0, 0, time.UTC)
	var submitted atomic.Int32
	var visible atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			submitted.Add(1)
			jsonReply(w, `{"success":true}`)
			return
		}
		if r.URL.Query().Get("month") != "2030-11" {
			t.Error("confirmation queried the current month instead of target month")
		}
		if visible.Load() {
			jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[{"checkin_date":"2030-11-30","quota_awarded":0}]}}}`)
		} else {
			jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[]}}}`)
		}
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "cookie", Cookie: "session=x"})
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if run.Status != "pending" {
		t.Fatal("unconfirmed native submission was accepted")
	}
	h.now = h.now.Add(2 * time.Minute)
	visible.Store(true)
	if _, err := h.s.Confirm(context.Background(), run.ID, "", false); err != nil {
		t.Fatal(err)
	}
	h.drain()
	run = h.run(account)
	if run.Status != "success" || run.RewardMicros == nil || *run.RewardMicros != 0 || submitted.Load() != 1 {
		t.Fatal("cross-month confirmation lost date or zero reward", run)
	}
	config := model.CheckinSiteConfig{Name: "legacy", BaseURL: server.URL, Adapter: "new-api", StatusMode: "disabled"}
	if err := model.NormalizeCheckinSite(&config); err != nil {
		t.Fatal(err)
	}
	adapter, _ := newPlatformAdapter(&adapterRuntime{site: model.CheckinSite{Config: config}})
	if adapter.Policy().Available {
		t.Fatal("native status opt-out ignored")
	}
}

func TestNewAPIStatusRequiresDatedConsistentEvidence(t *testing.T) {
	for _, body := range []string{
		`{"success":false,"data":{"enabled":true,"stats":{"checked_in_today":true,"records":[{"checkin_date":"2030-11-14"}]}}}`,
		`{"success":true,"data":{"enabled":false,"stats":{"checked_in_today":true,"records":[{"checkin_date":"2030-11-14"}]}}}`,
		`{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":true,"records":[]}}}`,
		`{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[{"checkin_date":"2030-11-14"}]}}}`,
		`{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":null}}}`,
	} {
		if result := newAPIStatus(HTTPResult{StatusCode: 200, Body: []byte(body)}, "2030-11-14", "2030-11-14"); result.State != "unknown" {
			t.Fatal("unreliable native status accepted", body)
		}
	}
}

func TestAnyRouterCookieFallbackAndPersistedIdentity(t *testing.T) {
	h := newHarness(t)
	var submits atomic.Int32
	var signIns atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Checkin-Adapter-Test/1.0" {
			t.Error("shared user agent lost")
		}
		switch r.URL.Path {
		case "/prefix/api/user/self":
			if r.Header.Get("Cookie") != "session=original" {
				t.Error("original cookie missing")
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "rotated-private", Path: "/prefix/"})
			jsonReply(w, `{"success":true,"data":{"id":42}}`)
		case "/prefix/api/user/sign_in", "/prefix/api/user/checkin":
			requireSubmitMarker(t, h)
			if !strings.Contains(r.Header.Get("Cookie"), "session=rotated-private") || r.Header.Get("New-Api-User") != "42" {
				t.Error("verified identity or renewed cookie missing")
			}
			submits.Add(1)
			if strings.HasSuffix(r.URL.Path, "/sign_in") {
				signIns.Add(1)
				w.WriteHeader(404)
				jsonReply(w, `{"success":false,"message":"route not found"}`)
				return
			}
			jsonReply(w, `{"success":true,"ret":1,"code":0,"data":{"reward":0}}`)
		default:
			t.Error("unexpected path", r.URL.Path)
		}
	}))
	defer server.Close()
	site, account := h.platformAccount(server.URL+"/prefix", "anyrouter", model.CheckinCredentials{Type: "cookie", Cookie: "session=original"})
	before := fingerprint(site, account)
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	latest, credentials := storedCredentials(t, h, account)
	if run.Status != "success" || run.RewardMicros == nil || *run.RewardMicros != 0 || signIns.Load() != 1 || submits.Load() != 2 {
		t.Fatal("AnyRouter fallback did not confirm once", run)
	}
	if latest.CredentialUserID != "42" || !latest.HasSession || credentials.Session == nil || latest.CredentialVersion <= account.CredentialVersion || fingerprint(site, latest) != before {
		t.Fatal("session renewal changed logical account identity")
	}
	raw, _ := json.Marshal(latest)
	if strings.Contains(string(raw), "rotated-private") || strings.Contains(latest.CredentialCipher, "rotated-private") {
		t.Fatal("cookie leaked from the vault")
	}
}

func TestAnyRouterDoesNotFallbackOrReplayAmbiguousSubmissions(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"server error", 503, `{"success":false}`}, {"limited", 429, `{"success":false}`},
		{"challenge", 403, `<html>cloudflare turnstile</html>`}, {"not JSON", 200, `<html>login</html>`},
		{"empty", 200, `{}`}, {"conflict", 200, `{"success":false,"code":0}`},
		{"malformed", 200, `{"ret":"1"}`}, {"disconnect", 0, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			var submits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					jsonReply(w, `{"success":true,"data":{"id":42}}`)
					return
				}
				submits.Add(1)
				if test.status == 0 {
					connection, _, _ := w.(http.Hijacker).Hijack()
					_ = connection.Close()
					return
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, account := h.platformAccount(server.URL, "anyrouter", model.CheckinCredentials{Type: "cookie", Cookie: "session=private"})
			h.enqueue(account, "manual")
			h.drain()
			run := h.run(account)
			if run.SafeToRetry || run.Status == "success" || run.SubmitIntentAt == nil {
				t.Fatal("ambiguous submission accepted or made retryable", run)
			}
			h.enqueue(account, "manual")
			h.drain()
			if submits.Load() != 1 {
				t.Fatal("ambiguous submission repeated")
			}
		})
	}
}

func TestAnyRouterBearerFallbackVerifiesCookieBeforeSubmitting(t *testing.T) {
	h := newHarness(t)
	var cookieVerified atomic.Bool
	var submits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			if r.Header.Get("Cookie") == "session=own-token" {
				cookieVerified.Store(true)
			}
			jsonReply(w, `{"success":true,"data":{"id":42}}`)
			return
		}
		submits.Add(1)
		requireSubmitMarker(t, h)
		if r.URL.Path == "/api/user/checkin" && r.Header.Get("Authorization") == "Bearer own-token" {
			w.WriteHeader(401)
			jsonReply(w, `{"success":false,"message":"session required"}`)
			return
		}
		if !cookieVerified.Load() || r.URL.Path != "/api/user/sign_in" || r.Header.Get("New-Api-User") != "42" || r.Header.Get("Cookie") != "session=own-token" {
			t.Error("fallback used unverified cookie authentication")
		}
		jsonReply(w, `{"ret":1}`)
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "anyrouter", model.CheckinCredentials{Type: "bearer", AccessToken: "Bearer own-token"})
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" || submits.Load() != 2 {
		t.Fatal("bearer-cookie fallback failed")
	}
}

func TestAnyRouterPasswordLoginReusesSessionAndPreservesLiteralSecrets(t *testing.T) {
	h := newHarness(t)
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			logins.Add(1)
			var input map[string]string
			if json.NewDecoder(r.Body).Decode(&input) != nil || input["username"] != "private-user@example.com" || input["password"] != `p{{user_id}}"{{checkin_date}}` {
				t.Error("login secrets were altered by request templates")
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "private-session", Path: "/"})
			jsonReply(w, `{"success":true,"data":{"id":42}}`)
		case "/api/user/self":
			jsonReply(w, `{"success":true,"data":{"id":42}}`)
		case "/api/user/sign_in":
			requireSubmitMarker(t, h)
			if r.Header.Get("Cookie") != "session=private-session" {
				t.Error("cached login cookie missing")
			}
			jsonReply(w, `{"code":0}`)
		default:
			t.Error("unexpected request")
		}
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "anyrouter", model.CheckinCredentials{Type: "password", Username: "private-user@example.com", Password: `p{{user_id}}"{{checkin_date}}`})
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" {
		t.Fatal("password login failed")
	}
	h.now = h.now.Add(24 * time.Hour)
	h.enqueue(account, "manual")
	h.drain()
	if logins.Load() != 1 {
		t.Fatal("saved session was not reused")
	}
	latest, _ := storedCredentials(t, h, account)
	raw, _ := json.Marshal(latest)
	if strings.Contains(string(raw), "private-") || strings.Contains(latest.CredentialCipher, "private-") || latest.CredentialType != "password" {
		t.Fatal("password/session leaked or replaced the credential type")
	}
}

func TestRouterIdentityMismatchStopsBeforeCheckin(t *testing.T) {
	h := newHarness(t)
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes.Add(1)
		}
		jsonReply(w, `{"success":true,"data":{"id":7}}`)
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "anyrouter", model.CheckinCredentials{Type: "cookie", Cookie: "session=private", UserID: "42"})
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Reason != "router_identity_mismatch" || writes.Load() != 0 {
		t.Fatal("identity mismatch reached a mutation")
	}
}

func TestConfiguredStatusCrossingMidnightCannotConfirmPreviousDay(t *testing.T) {
	h := newHarness(t)
	var clock atomic.Value
	clock.Store(h.now)
	h.s.Now = func() time.Time { return clock.Load().(time.Time) }
	var submitted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			submitted.Store(true)
			jsonReply(w, `{"success":true}`)
			return
		}
		if submitted.Load() {
			clock.Store(h.now.Add(24 * time.Hour))
			jsonReply(w, `{"success":true,"data":{"checked_in":true}}`)
		} else {
			jsonReply(w, `{"success":true,"data":{"checked_in":false}}`)
		}
	}))
	defer server.Close()
	site, account := h.account(server.URL, true, false)
	site.Config.Status.DatePath = ""
	if _, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config); err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if run.Status == "success" || run.Status == "already_checked_in" || run.RewardMicros != nil {
		t.Fatal("a date-less response crossed into yesterday's ledger", run)
	}
}

func agentLog(at int64) string {
	return fmt.Sprintf(`{"success":true,"data":{"items":[{"type":4,"content":"签到成功","created_at":%d}]}}`, at)
}
