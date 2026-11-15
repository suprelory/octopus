package checkin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

func mockTurnstile(t *testing.T, onTask func(*http.Request), onReady func()) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	count := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-solver-key" || r.Header.Get("Cookie") != "" || r.Header.Get("New-Api-User") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("solver credentials were mixed with account credentials")
		}
		switch r.URL.Path {
		case "/turnstile":
			id := count.Add(1)
			if onTask != nil {
				onTask(r)
			}
			jsonReply(w, fmt.Sprintf(`{"errorId":0,"taskId":"task-%d"}`, id))
		case "/result":
			if onReady != nil {
				onReady()
			}
			jsonReply(w, fmt.Sprintf(`{"errorId":0,"status":"ready","solution":{"token":"private-token-%s"}}`, r.URL.Query().Get("id")))
		default:
			t.Error("unexpected solver path")
		}
	}))
	t.Cleanup(server.Close)
	return server, count
}

func enableTurnstile(t *testing.T, h *harness, endpoint string) model.CheckinTurnstileSettings {
	t.Helper()
	previous, err := h.s.Store.TurnstileSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	config := previous.Config
	config.Enabled, config.CloudflareSolver.Enabled, config.CloudflareSolver.BaseURL = true, true, endpoint
	key := "private-solver-key"
	row, err := h.s.SaveTurnstile(context.Background(), TurnstileWrite{Config: config, Revision: previous.Revision, CloudflareKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func siteTurnstile(t *testing.T, h *harness, site model.CheckinSite, options model.CheckinTurnstileSite) model.CheckinSite {
	t.Helper()
	site.Config.Turnstile = &options
	saved, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestTurnstileWorksAcrossEveryPlatformAndPreservesDailyLedger(t *testing.T) {
	for _, platform := range model.CheckinPlatforms() {
		t.Run(platform.ID, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			var submitted, accepted atomic.Int32
			var site model.CheckinSite
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := strings.TrimPrefix(r.URL.Path, "/prefix")
				switch {
				case path == "/api/status":
					if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("New-Api-User") != "" {
						t.Error("discovery sent account credentials")
					}
					jsonReply(w, `{"success":true,"data":{"turnstile_site_key":"0x-public-site-key"}}`)
				case path == "/api/user/self":
					jsonReply(w, `{"success":true,"data":{"id":42,"quota":10}}`)
				case path == "/api/log/self/":
					jsonReply(w, `{"success":true,"data":{"items":[]}}`)
				case r.Method == "GET" && path == "/api/user/checkin":
					if accepted.Load() == 0 {
						jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[]}}}`)
					} else {
						jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":true,"records":[{"checkin_date":"2030-11-14","quota_awarded":5}]}}}`)
					}
				case r.Method == "POST" && (path == site.Config.Submit.Path || (platform.ID == "anyrouter" && path == "/api/user/checkin")):
					requireSubmitMarker(t, h)
					var run model.CheckinRun
					if h.s.Store.DB.First(&run).Error != nil || run.SafeToRetry {
						t.Error("token retry preceded its renewed durable marker")
					}
					submitted.Add(1)
					if platform.ID == "agentrouter" {
						var login map[string]string
						_ = json.NewDecoder(r.Body).Decode(&login)
						if login["password"] != "literal-{{checkin_date}}-{{user_id}}" || login["username"] != "login-user" {
							t.Error("Turnstile retry expanded login credentials as templates")
						}
					}
					if platform.ID != "agentrouter" && r.Header.Get("Authorization") != "Bearer private-account-bearer" {
						t.Error("retry lost account authentication")
					}
					if r.UserAgent() != "Checkin-Adapter-Test/1.0" {
						t.Error("retry changed session User-Agent")
					}
					if r.URL.Query().Get("turnstile") == "" {
						jsonReply(w, `{"success":false,"message":"Turnstile verification failed: private-account-bearer"}`)
						return
					}
					if r.URL.Query().Get("turnstile") != "private-token-task-1" {
						t.Error("wrong token")
					}
					accepted.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "private-router-session", Path: "/prefix"})
					jsonReply(w, `{"success":true,"code":0,"data":{"id":42,"checked_in":true,"reward":5}}`)
				default:
					t.Error("unexpected site request", r.Method, path)
				}
			}))
			defer server.Close()
			solver, creates := mockTurnstile(t, func(r *http.Request) {
				if r.URL.Query().Get("url") != server.URL+"/prefix/" || r.URL.Query().Get("sitekey") != "0x-public-site-key" {
					t.Error("solver received wrong public site parameters")
				}
			}, nil)
			enableTurnstile(t, h, solver.URL)
			credentials := model.CheckinCredentials{Type: "bearer", AccessToken: "private-account-bearer", UserID: "42"}
			if platform.ID == "agentrouter" {
				credentials = model.CheckinCredentials{Type: "password", Username: "login-user", Password: "literal-{{checkin_date}}-{{user_id}}"}
			}
			var account model.CheckinAccount
			site, account = h.platformAccount(server.URL+"/prefix", platform.ID, credentials)
			h.enqueue(account, "manual")
			h.drain()
			run := h.run(account)
			if run.Status != "success" || run.SafeToRetry || submitted.Load() != 2 || accepted.Load() != 1 || creates.Load() != 1 {
				t.Fatalf("platform did not solve and retry exactly once: %+v, calls=%d", run, submitted.Load())
			}
			attempts, _ := h.s.Store.Attempts(context.Background(), run.ID)
			raw, _ := json.Marshal(attempts)
			if strings.Contains(string(raw), "private-") || !strings.Contains(string(raw), "turnstile_solved") {
				t.Fatal("audit was missing or exposed secrets")
			}
			h.enqueue(account, "manual")
			h.drain()
			if submitted.Load() != 2 || creates.Load() != 1 {
				t.Fatal("completed check-in was repeated")
			}
		})
	}
}

func TestTurnstileDetectionRequiresAnExplicitRejection(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		want   bool
	}{
		{200, `{"success":false,"message":"Turnstile verification required"}`, true},
		{403, `{"error":{"code":"TURNSTILE_REQUIRED"}}`, true},
		{400, `{"message":"invalid turnstile token"}`, true},
		{200, `{"success":true,"message":"Turnstile enabled"}`, false},
		{200, `{"message":"Turnstile enabled"}`, false},
		{200, `{"success":false,"message":"captcha required"}`, false},
		{403, `<html>Cloudflare Just a moment /cdn-cgi/challenge-platform/ _cf_chl_opt</html>`, false},
		{403, `<html>_cf_chl_opt <div class="cf-turnstile" data-sitekey="0x-key"></div></html>`, false},
		{503, `<html><div class="cf-turnstile" data-sitekey="0x-key"></div></html>`, true},
		{200, `<html>Logged in<div class="cf-turnstile" data-sitekey="0x-key"></div></html>`, false},
		{429, `{"success":false,"message":"Turnstile rate limited"}`, false},
		{500, `{"success":false,"message":"Turnstile processing error"}`, false},
	} {
		if got := isTurnstileChallenge(HTTPResult{StatusCode: test.status, Body: []byte(test.body)}); got != test.want {
			t.Errorf("challenge classification %v, want %v: %s", got, test.want, test.body)
		}
	}
	if isTurnstileChallenge(HTTPResult{StatusCode: 403, Body: []byte(`{"success":false,"message":"Turnstile"}`), Err: errors.New("read interrupted")}) {
		t.Fatal("partial response authorized a retry")
	}
	for _, body := range []string{`<div class="cf-turnstile" data-sitekey="0x-key" data-action="checkin" data-cdata="data"></div>`, `<script>turnstile.render('#widget', {sitekey: '0x-key', action: 'checkin', cData: 'data'});</script>`} {
		params := widgetParameters([]byte(body))
		if params.SiteKey != "0x-key" || params.Action != "checkin" || params.CData != "data" {
			t.Fatal("static widget parameters not discovered", params)
		}
	}
}

func TestTurnstileRetryPreservesRequestAndSupportsAllTokenLocations(t *testing.T) {
	for _, location := range []string{"query", "json", "header"} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Query().Get("existing") != "keep" || r.Header.Get("Cookie") != "session=checkin-secret" || r.Header.Get("X-Custom") != "kept" {
					t.Error("retry changed request semantics or credentials")
				}
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["date"] != "2030-11-14" || body["user"] != "42" {
					t.Error("request templates not expanded")
				}
				if calls.Add(1) == 1 {
					jsonReply(w, `{"success":false,"message":"Turnstile required"}`)
					return
				}
				token := r.URL.Query().Get("captcha-response")
				if location == "json" {
					token = body["captcha-response"]
				}
				if location == "header" {
					token = r.Header.Get("captcha-response")
				}
				if token != "private-token-task-1" {
					t.Error("missing injected token")
				}
				jsonReply(w, `{"success":true}`)
			}))
			defer server.Close()
			solver, _ := mockTurnstile(t, nil, nil)
			enableTurnstile(t, h, solver.URL)
			site, account := h.account(server.URL, false, false)
			site.Config.Submit = model.CheckinRequest{Method: "GET", Path: "/submit?existing=keep", Body: `{"date":"{{checkin_date}}","user":"{{user_id}}"}`, Headers: []model.CheckinHeader{{Name: "X-Custom", Value: "kept"}}}
			siteTurnstile(t, h, site, model.CheckinTurnstileSite{SiteKey: "0x-fixed", TokenLocation: location, TokenField: "captcha-response"})
			h.enqueue(account, "manual")
			h.drain()
			if h.run(account).Status != "success" || calls.Load() != 2 {
				t.Fatal("injected retry did not finish")
			}
		})
	}
}

func TestTurnstileFailureRetainsSafeRetryOnlyForExplicitRejections(t *testing.T) {
	for _, mode := range []string{"disabled", "rejected", "network", "marker"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Query().Get("turnstile") != "" && mode == "network" {
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
					return
				}
				jsonReply(w, `{"success":false,"message":"Turnstile invalid: private-token"}`)
			}))
			defer server.Close()
			solver, creates := mockTurnstile(t, nil, nil)
			if mode != "disabled" {
				enableTurnstile(t, h, solver.URL)
			}
			site, account := h.account(server.URL, false, false)
			siteTurnstile(t, h, site, model.CheckinTurnstileSite{SiteKey: "0x-fixed"})
			if mode == "marker" {
				var writes atomic.Int32
				name := "test:deny_turnstile_retry_marker"
				_ = h.s.Store.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
					if values, ok := tx.Statement.Dest.(map[string]any); ok && values["phase"] == "submit_intent" && writes.Add(1) == 2 {
						tx.AddError(errors.New("cannot persist marker"))
					}
				})
				defer h.s.Store.DB.Callback().Update().Remove(name)
			}
			h.enqueue(account, "manual")
			h.s.ProcessOne(context.Background())
			run := h.run(account)
			wantCalls := int32(2)
			if mode == "disabled" || mode == "marker" {
				wantCalls = 1
			}
			if calls.Load() != wantCalls || run.SafeToRetry != (mode != "network") {
				t.Fatalf("unsafe retry state: %+v calls=%d", run, calls.Load())
			}
			if mode == "disabled" && (run.Reason != "turnstile_disabled" || creates.Load() != 0) {
				t.Fatal("disabled solver was called")
			}
			if mode == "rejected" && (run.Reason != "turnstile_rejected" || creates.Load() != 1) {
				t.Fatal("captcha rejection was solved repeatedly")
			}
			if mode == "network" {
				h.enqueue(account, "manual")
				h.drain()
				if calls.Load() != 2 || creates.Load() != 1 {
					t.Fatal("ambiguous token-bearing submit was replayed")
				}
			}
		})
	}
}

func TestTurnstileDoesNotRetryAfterCancellationOrBusinessDateChange(t *testing.T) {
	for _, mode := range []string{"cancel", "date"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			var now atomic.Int64
			now.Store(h.now.Unix())
			h.s.Now = func() time.Time { return time.Unix(now.Load(), 0).UTC() }
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				jsonReply(w, `{"success":false,"message":"Turnstile required"}`)
			}))
			defer server.Close()
			var jobID string
			solver, _ := mockTurnstile(t, nil, func() {
				if mode == "date" {
					now.Add(24 * 60 * 60)
				} else if err := h.s.Store.CancelJob(context.Background(), jobID); err != nil {
					t.Error(err)
				}
			})
			enableTurnstile(t, h, solver.URL)
			site, account := h.account(server.URL, false, false)
			siteTurnstile(t, h, site, model.CheckinTurnstileSite{SiteKey: "0x-fixed"})
			jobID = h.enqueue(account, "manual").ID
			h.drain()
			run := h.run(account)
			if calls.Load() != 1 || !run.SafeToRetry || run.Status != "skipped" {
				t.Fatalf("invalid late retry: %+v calls=%d", run, calls.Load())
			}
		})
	}
}

func TestTurnstileProviderFallbackAndReadOnlyInspection(t *testing.T) {
	h := newHarness(t)
	var first, second, reads atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		jsonReply(w, `{"errorId":1,"errorCode":"ERROR_ZERO_BALANCE","errorDescription":"private-solver-key"}`)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "POST" || body["clientKey"] != "fallback-key" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("invalid fallback credentials")
		}
		if r.URL.Path == "/createTask" {
			second.Add(1)
			jsonReply(w, `{"errorId":0,"taskId":"fallback"}`)
		} else {
			jsonReply(w, `{"errorId":0,"status":"ready","solution":{"token":"fallback-token"}}`)
		}
	}))
	defer fallback.Close()
	settings := enableTurnstile(t, h, primary.URL)
	settings.Config.SolverQ = model.CheckinTurnstileProvider{Enabled: true, BaseURL: fallback.URL}
	key := "fallback-key"
	if _, err := h.s.SaveTurnstile(context.Background(), TurnstileWrite{Config: settings.Config, Revision: settings.Revision, SolverQKey: &key}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/user/self" {
			t.Error("balance inspection performed a mutation")
		}
		reads.Add(1)
		if r.URL.Query().Get("turnstile") == "" {
			jsonReply(w, `{"success":false,"message":"Turnstile required","sitekey":"0x-public"}`)
			return
		}
		if r.URL.Query().Get("turnstile") != "fallback-token" {
			t.Error("fallback token missing")
		}
		jsonReply(w, `{"success":true,"data":{"id":42,"quota":12500,"used_quota":100}}`)
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "bearer", AccessToken: "private-site-token", UserID: "42"})
	updated, err := h.s.InspectAccount(context.Background(), account.ID, "balance")
	if err != nil || updated.Inspection.Status != "success" || updated.Balance == nil || reads.Load() != 2 || first.Load() != 1 || second.Load() != 1 {
		t.Fatal("read-only fallback did not finish", err, updated.Inspection)
	}
	var runs int64
	h.s.Store.DB.Model(&model.CheckinRun{}).Count(&runs)
	if runs != 0 {
		t.Fatal("inspection created a check-in ledger")
	}
}

func TestTurnstileSub2APIRefreshPreservesTokensBeforeAudit(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "audit", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var refreshes atomic.Int32
			var h *harness
			var account model.CheckinAccount
			h, site, savedAccount := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/refresh" {
					refreshes.Add(1)
					var row model.CheckinAccount
					if h.s.Store.DB.First(&row, "id = ?", account.ID).Error != nil || !row.RefreshPending {
						t.Error("refresh retried before persisting its marker")
					}
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["refresh_token"] != "old-refresh" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
						t.Error("refresh changed its original credentials")
					}
					if r.URL.Query().Get("turnstile") == "" || mode == "rejected" {
						w.WriteHeader(403)
						jsonReply(w, `{"error":{"code":"TURNSTILE_REQUIRED"}}`)
						return
					}
					if mode == "cancel" {
						cancel()
					}
					sub2APITokens(w)
					return
				}
				if r.Header.Get("Authorization") != "Bearer new-access" {
					t.Error("new access token not used")
				}
				jsonReply(w, `{"code":0}`)
			}, false, true)
			account = savedAccount
			solver, _ := mockTurnstile(t, nil, nil)
			enableTurnstile(t, h, solver.URL)
			siteTurnstile(t, h, site, model.CheckinTurnstileSite{SiteKey: "0x-refresh"})
			if mode == "audit" {
				name := "test:turnstile_refresh_audit_failure"
				_ = h.s.Store.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
					if row, ok := tx.Statement.Dest.(*model.CheckinAttempt); ok && row.Stage == "refresh" && row.Reason == "response_received" {
						tx.AddError(errors.New("audit unavailable"))
					}
				})
				defer h.s.Store.DB.Callback().Create().Remove(name)
			}
			h.enqueue(account, "manual")
			h.s.ProcessOne(ctx)
			latest, credentials := storedCredentials(t, h, account)
			if refreshes.Load() != 2 || latest.RefreshPending {
				t.Fatal("refresh retry or pending state is incorrect")
			}
			if mode == "rejected" {
				if credentials.RefreshToken != "old-refresh" || latest.RefreshError != "" || h.run(account).Reason != "turnstile_rejected" {
					t.Fatal("captcha refusal invalidated an unused refresh token")
				}
			} else if credentials.RefreshToken != "new-refresh" || latest.CredentialVersion != account.CredentialVersion+1 {
				t.Fatal("new refresh pair was lost after a successful retry")
			}
		})
	}
}

func TestTurnstileWorkerBudgetFitsLease(t *testing.T) {
	// Rotation may finish for 20 seconds after the parent is canceled, followed
	// by bounded credential, audit and result saves, all within the account lease.
	if checkinOperationTimeout+35*time.Second >= op.CheckinLeaseDuration {
		t.Fatal("solver timeout can outlive the account lease")
	}
}

func TestTurnstileUsesFreshTokensAcrossQueryAndSubmitStages(t *testing.T) {
	for _, mode := range []string{"checkin", "detect"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			var submits atomic.Int32
			var tokens atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("turnstile") == "" {
					jsonReply(w, `{"success":false,"message":"Turnstile required","sitekey":"0x-public"}`)
					return
				}
				expected := fmt.Sprintf("private-token-task-%d", tokens.Add(1))
				if r.URL.Query().Get("turnstile") != expected {
					t.Error("a solved token was reused between requests")
				}
				if r.Method == "POST" {
					submits.Add(1)
					jsonReply(w, `{"success":true}`)
					return
				}
				if r.URL.Path == "/api/user/self" {
					jsonReply(w, `{"success":true,"data":{"id":42,"quota":200}}`)
					return
				}
				if submits.Load() == 0 {
					jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[]}}}`)
				} else {
					jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"records":[{"checkin_date":"2030-11-14","quota_awarded":5}]}}}`)
				}
			}))
			defer server.Close()
			solver, creates := mockTurnstile(t, nil, nil)
			enableTurnstile(t, h, solver.URL)
			_, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "bearer", AccessToken: "site-token", UserID: "42"})
			if mode == "checkin" {
				h.enqueue(account, "manual")
				h.drain()
				if h.run(account).Status != "success" || creates.Load() != 3 || submits.Load() != 1 {
					t.Fatal("check-in stages did not solve independently")
				}
			} else {
				updated, err := h.s.InspectAccount(context.Background(), account.ID, "detect")
				if err != nil || updated.Inspection.Status != "success" || updated.Inspection.CheckinStatus != "not_checked_in" || creates.Load() != 2 || submits.Load() != 0 {
					t.Fatal("detection lost its read-only contract", err, updated.Inspection)
				}
			}
		})
	}
}

func TestTurnstilePublicPageDiscoveryAndScopedChallengeCookies(t *testing.T) {
	h := newHarness(t)
	var submissions, discovery atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/submit" {
			discovery.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("New-Api-User") != "" {
				t.Error("public discovery sent account identity")
			}
			if r.URL.Path == "/prefix/api/status" {
				w.WriteHeader(404)
				return
			}
			if r.URL.Path != "/prefix/login" {
				t.Error("discovery left the configured page")
			}
			_, _ = w.Write([]byte(`<script>turnstile.render('#widget', {sitekey: '0x-page', action: 'login', cData: 'page-data'});</script>`))
			return
		}
		if submissions.Add(1) == 1 {
			http.SetCookie(w, &http.Cookie{Name: "captcha_nonce", Value: "nonce", Path: "/prefix"})
			http.SetCookie(w, &http.Cookie{Name: "unrelated", Value: "no", Path: "/different"})
			http.SetCookie(w, &http.Cookie{Name: "foreign", Value: "no", Domain: "external.invalid"})
			http.SetCookie(w, &http.Cookie{Name: "secure", Value: "no", Secure: true})
			jsonReply(w, `{"success":false,"message":"Turnstile verification required"}`)
			return
		}
		for key, want := range map[string]string{"session": "checkin-secret", "captcha_nonce": "nonce"} {
			cookie, err := r.Cookie(key)
			if err != nil || cookie.Value != want {
				t.Error("retry lost an original or challenge cookie", key)
			}
		}
		for _, key := range []string{"unrelated", "foreign", "secure"} {
			if _, err := r.Cookie(key); err == nil {
				t.Error("retry widened a challenge cookie", key)
			}
		}
		if r.Header.Get("CF-Turnstile-Response") != "private-token-task-1" || r.URL.RawQuery != "original=kept" {
			t.Error("header token changed original request URL")
		}
		jsonReply(w, `{"success":true}`)
	}))
	defer server.Close()
	solver, creates := mockTurnstile(t, func(r *http.Request) {
		query := r.URL.Query()
		if query.Get("url") != server.URL+"/prefix/login" || query.Get("sitekey") != "0x-page" || query.Get("action") != "checkin" || query.Get("cdata") != "page-data" {
			t.Error("wrong discovered or overridden widget parameters")
		}
	}, nil)
	enableTurnstile(t, h, solver.URL)
	site, account := h.account(server.URL+"/prefix", false, false)
	site.Config.Submit.Path = "/submit?original=kept"
	siteTurnstile(t, h, site, model.CheckinTurnstileSite{PagePath: "/login", Action: "checkin", TokenLocation: "header", TokenField: "CF-Turnstile-Response"})
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" || discovery.Load() != 2 || submissions.Load() != 2 || creates.Load() != 1 {
		t.Fatal("public page discovery did not produce one authenticated retry")
	}
}
