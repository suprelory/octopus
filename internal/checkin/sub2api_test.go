package checkin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

func sub2APIReply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func sub2APITokens(w http.ResponseWriter) {
	sub2APIReply(w, 200, map[string]any{"code": 0, "data": map[string]any{
		"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 3600, "token_type": "Bearer",
	}})
}

func sub2APIStatus(w http.ResponseWriter, checked bool) {
	sub2APIReply(w, 200, map[string]any{"code": 0, "data": map[string]any{
		"checked_in": checked, "date": "2030-11-14", "reward": "0.125",
	}})
}

func newSub2APIHarness(t *testing.T, handler http.HandlerFunc, status, expired bool) (*harness, model.CheckinSite, model.CheckinAccount) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	h := newHarness(t)
	site, account := h.account(server.URL, status, false)
	config := site.Config
	config.Adapter, config.UserAgent = "sub2api", "Sub2API-Test/1.0"
	site, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, config)
	if err != nil {
		t.Fatal(err)
	}
	credentials := &model.CheckinCredentials{Type: "bearer", AccessToken: "old-access", RefreshToken: "old-refresh"}
	if expired {
		at := h.now.Add(-time.Minute)
		credentials.ExpiresAt = &at
	}
	account, err = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: account.CredentialVersion, Credentials: credentials})
	if err != nil {
		t.Fatal(err)
	}
	return h, site, account
}

func TestSub2APIRotationPersistsEncryptedTokensAndIdentity(t *testing.T) {
	var refreshes, submits atomic.Int32
	h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "Sub2API-Test/1.0" {
			t.Errorf("different session User-Agent: %q", r.UserAgent())
		}
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshes.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || body["refresh_token"] != "old-refresh" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("refresh must use its own token without the expired Authorization or Cookie")
			}
			sub2APITokens(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Error("request did not use the rotated Access Token")
		}
		if r.URL.Path == "/submit" {
			submits.Add(1)
			sub2APIReply(w, 200, map[string]any{"code": 0})
			return
		}
		sub2APIStatus(w, submits.Load() > 0)
	}, true, true)
	initialFingerprint := fingerprint(site, account)
	// Simulate an account created before identity versions were introduced.
	if err := h.s.Store.DB.Model(&model.CheckinAccount{}).Where("id = ?", account.ID).Update("credential_identity_version", 0).Error; err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if run.Status != "success" || run.RewardMicros == nil || *run.RewardMicros != 125000 {
		t.Fatalf("unexpected result: %+v", run)
	}
	updated, err := h.s.Store.Account(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CredentialVersion != account.CredentialVersion+1 || updated.IdentityVersion() != account.IdentityVersion() || fingerprint(site, updated) != initialFingerprint || updated.RefreshPending || !updated.HasRefreshToken {
		t.Fatal("rotation changed identity or did not finish atomically")
	}
	plain, err := h.s.Vault.Unseal(updated.ID, updated.CredentialCipher)
	if err != nil || !strings.Contains(string(plain), "new-refresh") || updated.TokenExpiresAt == nil || updated.TokenRefreshedAt == nil {
		t.Fatal("rotated pair or expiry was not saved")
	}
	public, _ := json.Marshal(updated)
	for _, secret := range []string{"old-access", "old-refresh", "new-access", "new-refresh", updated.CredentialCipher} {
		if strings.Contains(string(public), secret) {
			t.Fatal("account API exposes session credentials")
		}
	}
	// A form opened before renewal cannot overwrite the new pair.
	_, err = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: account.CredentialVersion, Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: "manual"}})
	if !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatalf("stale credential edit was accepted: %v", err)
	}
	h.enqueue(updated, "manual")
	h.drain()
	if refreshes.Load() != 1 || submits.Load() != 1 {
		t.Fatal("daily reuse repeated an upstream operation")
	}
	attempts, _ := h.s.Store.Attempts(context.Background(), run.ID)
	found := false
	for _, attempt := range attempts {
		found = found || attempt.Stage == "refresh"
	}
	if !found {
		t.Fatal("refresh is missing from the request stages")
	}
}

func TestSub2APIQuery401RefreshesAndRetriesQueryOnce(t *testing.T) {
	var refreshes, oldQueries, submits atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshes.Add(1)
			sub2APITokens(w)
		case "/status":
			if r.Header.Get("Authorization") == "Bearer old-access" {
				oldQueries.Add(1)
				sub2APIReply(w, 401, map[string]any{"code": 401, "reason": "TOKEN_EXPIRED"})
				return
			}
			sub2APIStatus(w, submits.Load() > 0)
		case "/submit":
			submits.Add(1)
			sub2APIReply(w, 200, map[string]any{"code": 0})
		}
	}, true, false)
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" || refreshes.Load() != 1 || oldQueries.Load() != 1 || submits.Load() != 1 {
		t.Fatal("query recovery failed or repeated submission")
	}
}

func TestSub2APISubmit401NeverReplaysGETOrPOST(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		for _, status := range []bool{false, true} {
			name := method
			if status {
				name += "_with_status"
			}
			t.Run(name, func(t *testing.T) {
				var refreshes, submits atomic.Int32
				h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/auth/refresh":
						refreshes.Add(1)
						sub2APITokens(w)
					case "/status":
						sub2APIStatus(w, submits.Load() > 0)
					case "/submit":
						submits.Add(1)
						sub2APIReply(w, 401, map[string]any{"code": 401, "reason": "TOKEN_EXPIRED"})
					}
				}, status, false)
				site.Config.Submit.Method = method
				if _, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config); err != nil {
					t.Fatal(err)
				}
				h.enqueue(account, "manual")
				h.drain()
				run := h.run(account)
				expected := "manual_required"
				if status {
					expected = "success"
				}
				if run.Status != expected || run.SafeToRetry || run.SubmitIntentAt == nil {
					t.Fatalf("unsafe submit recovery: %+v", run)
				}
				h.enqueue(account, "manual")
				h.drain()
				if refreshes.Load() != 1 || submits.Load() != 1 {
					t.Fatal("authentication recovery replayed a submitted check-in")
				}
			})
		}
	}
}

func TestSub2APIConfirmationKeepsEvidenceAcrossRotation(t *testing.T) {
	var checked atomic.Bool
	var refreshes, submits atomic.Int32
	h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshes.Add(1)
			sub2APITokens(w)
		case "/submit":
			submits.Add(1)
			sub2APIReply(w, 200, map[string]any{"code": 0})
		case "/status":
			sub2APIStatus(w, checked.Load())
		}
	}, true, false)
	expiry := h.now.Add(10 * time.Minute)
	account, _ = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: account.CredentialVersion, Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expiry}})
	h.enqueue(account, "manual")
	h.drain()
	before := h.run(account)
	if before.Status != "pending" || !before.BeforeUnchecked || refreshes.Load() != 0 {
		t.Fatal("expected a submitted, unconfirmed run")
	}
	h.now = h.now.Add(11 * time.Minute)
	checked.Store(true)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	after := h.run(account)
	if after.ID != before.ID || after.Status != "success" || after.Fingerprint != before.Fingerprint || after.RewardMicros == nil || refreshes.Load() != 1 || submits.Load() != 1 {
		t.Fatalf("rotation invalidated confirmation evidence: %+v", after)
	}
}

func TestSub2APIPermanentAuthenticationFailuresDoNotRefresh(t *testing.T) {
	for _, reason := range []string{"TOKEN_REVOKED", "SESSION_BINDING_MISMATCH", "USER_INACTIVE", "USER_NOT_FOUND", "UNKNOWN_REASON"} {
		t.Run(reason, func(t *testing.T) {
			var refreshes atomic.Int32
			h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/refresh" {
					refreshes.Add(1)
				}
				sub2APIReply(w, 401, map[string]any{"code": 401, "reason": reason})
			}, true, false)
			h.enqueue(account, "manual")
			h.drain()
			if h.run(account).Status != "manual_required" || refreshes.Load() != 0 {
				t.Fatal("permanent authentication failure triggered refresh")
			}
		})
	}
}

func TestSub2APIRefresh429BacksOffWithoutDiscardingCredentials(t *testing.T) {
	var refreshes atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			if refreshes.Add(1) == 1 {
				sub2APIReply(w, 429, map[string]any{"code": 429})
			} else {
				sub2APITokens(w)
			}
			return
		}
		sub2APIReply(w, 200, map[string]any{"code": 0})
	}, false, true)
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "pending" {
		t.Fatal("rate limit should defer the unsubmitted operation")
	}
	h.enqueue(account, "manual")
	h.drain()
	if refreshes.Load() != 1 {
		t.Fatal("refresh ignored its backoff")
	}
	h.now = h.now.Add(2 * time.Minute)
	h.enqueue(account, "manual")
	h.drain()
	if refreshes.Load() != 2 || h.run(account).Status != "success" {
		t.Fatal("refresh did not resume after backoff")
	}
}

func TestSub2APIUnknownRotationNeverReplaysAfterRestart(t *testing.T) {
	var refreshes atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		// A broken response may follow consumption of the old refresh token.
		sub2APIReply(w, 200, map[string]any{"code": 0, "data": map[string]any{"access_token": "incomplete"}})
	}, false, true)
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Reason != "sub2api_refresh_uncertain" {
		t.Fatal("ambiguous rotation was accepted")
	}
	store, vault := h.s.Store, h.s.Vault
	_ = h.s.Close()
	h.s = New(store, vault)
	h.s.Now = func() time.Time { return h.now }
	h.enqueue(account, "manual")
	h.drain()
	if refreshes.Load() != 1 {
		t.Fatal("restart reused a possibly consumed refresh token")
	}
}

func TestSub2APIRefreshRequiresDurableIntent(t *testing.T) {
	var requests atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		sub2APITokens(w)
	}, false, true)
	callback := "test:deny_refresh_intent"
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if updates, ok := tx.Statement.Dest.(map[string]any); ok && updates["refresh_pending"] == true {
			tx.AddError(errors.New("intent storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.s.Store.DB.Callback().Update().Remove(callback)
	h.enqueue(account, "manual")
	h.drain()
	if requests.Load() != 0 {
		t.Fatal("refresh token was consumed before its marker was durable")
	}
}

func TestSub2APIReceivedPairRetriesOnlySaving(t *testing.T) {
	var refreshes atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshes.Add(1)
			sub2APITokens(w)
		} else {
			sub2APIReply(w, 200, map[string]any{"code": 0})
		}
	}, false, true)
	var deny atomic.Bool
	deny.Store(true)
	callback := "test:deny_session_save"
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if updates, ok := tx.Statement.Dest.(map[string]any); ok && deny.Load() && updates["credential_cipher"] != nil {
			tx.AddError(errors.New("temporary database write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.s.Store.DB.Callback().Update().Remove(callback)
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Reason != "sub2api_refresh_save_failed" {
		t.Fatal("expected a recoverable session save failure")
	}
	deny.Store(false)
	h.enqueue(account, "manual")
	h.drain()
	if refreshes.Load() != 1 || h.run(account).Status != "success" {
		t.Fatal("received tokens were discarded or exchanged again")
	}
}

func TestSub2APIStartedRotationSurvivesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var submits atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			cancel()
			sub2APITokens(w)
		} else {
			submits.Add(1)
		}
	}, false, true)
	h.enqueue(account, "manual")
	if !h.s.ProcessOne(ctx) {
		t.Fatal("worker did not start")
	}
	latest, _ := h.s.Store.Account(context.Background(), account.ID)
	if latest.CredentialVersion != account.CredentialVersion+1 || latest.RefreshPending || submits.Load() != 0 {
		t.Fatal("caller cancellation lost the rotated session or submitted new work")
	}
}

func TestSub2APIConcurrentEntriesRotateOnce(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var refreshes atomic.Int32
	h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			if refreshes.Add(1) == 1 {
				close(entered)
			}
			<-release
			sub2APITokens(w)
			return
		}
		sub2APIReply(w, 200, map[string]any{"code": 0})
	}, false, true)
	h.enqueue(account, "manual")
	if _, err := h.s.Enqueue(context.Background(), model.CheckinScope{Kind: "sites", IDs: []string{site.ID}}, "manual", ""); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); h.s.ProcessOne(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("refresh did not start")
	}
	_, editErr := h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: account.CredentialVersion, Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: "manual"}})
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); h.s.ProcessOne(context.Background()) }()
	}
	close(release)
	workers.Wait()
	h.drain()
	if !errors.Is(editErr, op.ErrCheckinConflict) || refreshes.Load() != 1 || h.run(account).Status != "success" {
		t.Fatal("concurrent work did not share the leased account session")
	}
}

func TestSub2APIExpiryUsesJWTWithoutTrustingClaimsForAuthentication(t *testing.T) {
	now := time.Date(2030, 11, 14, 12, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(map[string]any{"exp": now.Add(time.Minute).Unix(), "iat": now.Unix()})
	token := "header." + base64.RawURLEncoding.EncodeToString(body) + ".unverified"
	later := now.Add(time.Hour)
	credentials := model.CheckinCredentials{Type: "bearer", AccessToken: "Bearer " + token, ExpiresAt: &later}
	normalizeSub2APICredentials(&credentials)
	if credentials.AccessToken != token || !credentials.ExpiresAt.Equal(now.Add(time.Minute)) || sub2APIRefreshDue(credentials, model.CheckinAccount{}, now) || !sub2APIRefreshDue(credentials, model.CheckinAccount{}, now.Add(49*time.Second)) {
		t.Fatal("JWT expiry or short-lived token refresh window is incorrect")
	}
}

func TestSub2APIStatusRejectsBusinessFailureWithStaleCheckedFlag(t *testing.T) {
	config := model.CheckinSiteConfig{Adapter: "sub2api", Status: &model.CheckinStatusRule{
		Checked:   model.CheckinMatch{Path: "data.checked_in", Value: json.RawMessage("true")},
		Unchecked: model.CheckinMatch{Path: "data.checked_in", Value: json.RawMessage("false")},
	}}
	for _, code := range []any{1, "0", 0} {
		body, _ := json.Marshal(map[string]any{"code": code, "data": map[string]any{"checked_in": true}})
		want := "unknown"
		if code == 0 {
			want = "checked_in"
		}
		if got := evaluateSiteStatus(config, 200, body, "2030-11-14"); got != want {
			t.Fatalf("code %v: got %s, want %s", code, got, want)
		}
	}
}

func TestSub2APITemporaryOrLostRefreshResponseDoesNotReuseToken(t *testing.T) {
	for _, mode := range []string{"server_error", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if mode == "disconnect" {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = connection.Close()
					}
					return
				}
				sub2APIReply(w, 503, map[string]any{"code": 503, "reason": "SERVICE_UNAVAILABLE"})
			}, false, true)
			h.enqueue(account, "manual")
			h.drain()
			h.enqueue(account, "manual")
			h.drain()
			if requests.Load() != 1 || h.run(account).Reason != "sub2api_refresh_uncertain" {
				t.Fatal("an ambiguous refresh response caused token replay")
			}
		})
	}
}

func TestSub2APIRejectsRotationIntoAnotherUserOrSession(t *testing.T) {
	for _, changed := range []string{"user_id", "sid"} {
		t.Run(changed, func(t *testing.T) {
			now := time.Date(2030, 11, 14, 12, 0, 0, 0, time.UTC)
			jwt := func(id int, sid string, expiry time.Time) string {
				body, _ := json.Marshal(map[string]any{"user_id": id, "sid": sid, "exp": expiry.Unix()})
				return "header." + base64.RawURLEncoding.EncodeToString(body) + ".signature"
			}
			next := jwt(2, "family-a", now.Add(time.Hour))
			if changed == "sid" {
				next = jwt(1, "family-b", now.Add(time.Hour))
			}
			var requests atomic.Int32
			h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				sub2APIReply(w, 200, map[string]any{"code": 0, "data": map[string]any{"access_token": next, "refresh_token": "new-refresh", "expires_in": 3600}})
			}, true, true)
			var err error
			account, err = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{
				Revision: account.Revision, CredentialVersion: account.CredentialVersion,
				Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: jwt(1, "family-a", now.Add(-time.Minute)), RefreshToken: "old-refresh"},
			})
			if err != nil {
				t.Fatal(err)
			}
			h.enqueue(account, "manual")
			h.drain()
			h.enqueue(account, "manual")
			h.drain()
			latest, _ := h.s.Store.Account(context.Background(), account.ID)
			if requests.Load() != 1 || h.run(account).Reason != "sub2api_refresh_identity_mismatch" || latest.CredentialVersion != account.CredentialVersion {
				t.Fatal("automatic refresh silently changed the check-in identity")
			}
		})
	}
}
