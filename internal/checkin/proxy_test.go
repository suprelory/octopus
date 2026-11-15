package checkin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func bindCheckinProxy(t *testing.T, h *harness, site model.CheckinSite, node model.ProxyConfiguration) model.CheckinSite {
	t.Helper()
	site.Config.ProxyMode, site.Config.ProxyConfigID = "pool", &node.ID
	updated, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestInspectionKeepsProxySnapshotThroughTokenRefresh(t *testing.T) {
	h := newHarness(t)
	var originalRequests, changedRequests atomic.Int32
	var node model.ProxyConfiguration
	changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		changedRequests.Add(1)
		jsonReply(w, `{"code":0,"data":{"id":42,"balance":2}}`)
	}))
	defer changed.Close()
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalRequests.Add(1)
		if r.URL.Host != "inspection.invalid" || r.UserAgent() != "Checkin-Adapter-Test/1.0" {
			t.Error("inspection lost its target or User-Agent")
		}
		if r.URL.Path == "/prefix/api/v1/auth/refresh" {
			if _, err := op.ProxySave(context.Background(), h.s.Store.DB, node.ID, model.ProxyWrite{Name: node.Name, URL: changed.URL, Enabled: true, Revision: node.Revision}); err != nil {
				t.Error(err)
			}
			sub2APITokens(w)
			return
		}
		if r.URL.Path != "/prefix/api/v1/auth/me" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer new-access" {
			t.Error("inspection did not reuse the refreshed session")
		}
		jsonReply(w, `{"code":0,"data":{"id":42,"balance":1}}`)
	}))
	defer original.Close()
	var err error
	node, err = op.ProxySave(context.Background(), h.s.Store.DB, 0, model.ProxyWrite{Name: "Inspection route", URL: original.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	expired := h.now.Add(-time.Minute)
	site, account := h.platformAccount("http://inspection.invalid/prefix", "sub2api", model.CheckinCredentials{Type: "bearer", AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expired})
	bindCheckinProxy(t, h, site, node)
	result := inspectAccount(t, h, account, "detect")
	if result.Inspection.Status != "success" || originalRequests.Load() != 2 || changedRequests.Load() != 0 {
		t.Fatal("inspection changed proxies between refresh and account query")
	}
	inspectAccount(t, h, account, "balance")
	if originalRequests.Load() != 2 || changedRequests.Load() != 1 {
		t.Fatal("next inspection did not resolve the updated proxy")
	}
}

func TestCheckinProxySnapshotSurvivesNodeEditDuringSubmission(t *testing.T) {
	h := newHarness(t)
	var oldRequests, newRequests, submits atomic.Int32
	var node model.ProxyConfiguration
	proxyB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newRequests.Add(1); jsonReply(w, `{}`) }))
	defer proxyB.Close()
	proxyA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldRequests.Add(1)
		if r.URL.Host != "checkin.invalid" || r.Header.Get("Cookie") != "session=checkin-secret" {
			t.Error("proxy lost target or account identity")
		}
		if r.URL.Path == "/submit" {
			submits.Add(1)
			if _, err := op.ProxySave(context.Background(), h.s.Store.DB, node.ID, model.ProxyWrite{Name: node.Name, URL: proxyB.URL, Enabled: true, Revision: node.Revision}); err != nil {
				t.Error(err)
			}
			jsonReply(w, `{"success":true}`)
			return
		}
		jsonReply(w, fmt.Sprintf(`{"data":{"checked_in":%t,"date":"%s","reward":3}}`, submits.Load() > 0, businessDate(model.CheckinSiteConfig{Timezone: "UTC"}, h.now)))
	}))
	defer proxyA.Close()
	var err error
	node, err = op.ProxySave(context.Background(), h.s.Store.DB, 0, model.ProxyWrite{Name: "Stable execution", URL: proxyA.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	site, account := h.account("http://checkin.invalid", true, false)
	bindCheckinProxy(t, h, site, node)
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if run.Status != "success" || submits.Load() != 1 || oldRequests.Load() != 3 || newRequests.Load() != 0 || run.ProxyFingerprint == "" {
		t.Fatal("execution changed proxy or repeated submission", run, oldRequests.Load(), newRequests.Load())
	}
}

func TestChangedProxyPendingRunRequiresConfirmationWithoutResubmission(t *testing.T) {
	h := newHarness(t)
	var submits, confirmations atomic.Int32
	proxyA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			submits.Add(1)
			jsonReply(w, `{}`)
			return
		}
		if submits.Load() == 0 {
			jsonReply(w, `{"data":{"checked_in":false}}`)
		} else {
			jsonReply(w, `{}`)
		}
	}))
	defer proxyA.Close()
	proxyB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		confirmations.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/status" {
			t.Error("recovery performed a mutation through new proxy")
		}
		jsonReply(w, fmt.Sprintf(`{"data":{"checked_in":true,"date":"%s","reward":3}}`, h.now.Format("2006-01-02")))
	}))
	defer proxyB.Close()
	node, err := op.ProxySave(context.Background(), h.s.Store.DB, 0, model.ProxyWrite{Name: "Pending execution", URL: proxyA.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	site, account := h.account("http://checkin.invalid", true, false)
	// This fixture supports same-day confirmation without date evidence.
	site.Config.Status.DatePath = ""
	site = bindCheckinProxy(t, h, site, node)
	h.enqueue(account, "manual")
	h.drain()
	before := h.run(account)
	if before.Status != "pending" || before.SubmitIntentAt == nil || submits.Load() != 1 {
		t.Fatal("pending fixture failed", before)
	}
	if _, err := op.ProxySave(context.Background(), h.s.Store.DB, node.ID, model.ProxyWrite{Name: node.Name, URL: proxyB.URL, Enabled: true, Revision: node.Revision}); err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	after := h.run(account)
	if after.Status != "manual_required" || after.Reason != "config_changed" || after.ID != before.ID || confirmations.Load() != 0 || submits.Load() != 1 {
		t.Fatal("changed proxy bypassed pending-run protection", after)
	}
	if _, err := h.s.Confirm(context.Background(), after.ID, "", false); err != nil {
		t.Fatal(err)
	}
	h.drain()
	after = h.run(account)
	if after.Status != "already_checked_in" || submits.Load() != 1 || confirmations.Load() != 1 || after.BeforeUnchecked || after.RewardMicros != nil {
		t.Fatal("confirmation replayed submission or reused old reward evidence", after)
	}
}

func TestCheckinAccountDirectOverrideAndExplicitSystemProxy(t *testing.T) {
	h := newHarness(t)
	var directRequests, systemRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directRequests.Add(1); jsonReply(w, `{"success":true}`) }))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { systemRequests.Add(1); jsonReply(w, `{"success":true}`) }))
	defer proxy.Close()
	node, err := op.ProxySave(context.Background(), h.s.Store.DB, 0, model.ProxyWrite{Name: "Disabled inherited proxy", URL: proxy.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	site, account := h.account(target.URL, false, false)
	bindCheckinProxy(t, h, site, node)
	if _, err := op.ProxySave(context.Background(), h.s.Store.DB, node.ID, model.ProxyWrite{Name: node.Name, URL: node.URL, Enabled: false, Revision: node.Revision}); err != nil {
		t.Fatal(err)
	}
	mode := "direct"
	account, err = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, ProxyMode: &mode})
	if err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" || directRequests.Load() != 1 || systemRequests.Load() != 0 {
		t.Fatal("direct account inherited a disabled proxy")
	}
	_, second := h.account(target.URL, false, false)
	if err := h.s.Store.DB.Create(&model.Setting{Key: model.SettingKeyProxyURL, Value: proxy.URL}).Error; err != nil {
		t.Fatal(err)
	}
	mode = "system"
	second, err = h.s.SaveAccount(context.Background(), second.SiteID, second.ID, AccountWrite{Revision: second.Revision, ProxyMode: &mode})
	if err != nil {
		t.Fatal(err)
	}
	h.enqueue(second, "manual")
	h.drain()
	if h.run(second).Status != "success" || systemRequests.Load() != 1 || directRequests.Load() != 1 {
		t.Fatal("system mode did not use application proxy")
	}
}

func TestProxyIDRemappingPreservesCheckinExecutionIdentity(t *testing.T) {
	h := newHarness(t)
	site, account := h.account("http://checkin.invalid", true, false)
	oldID, newID := 1, 42
	site.Config.ProxyMode, site.Config.ProxyConfigID = "pool", &oldID
	account.ProxyMode, account.ProxyConfigID = "pool", &oldID
	before := fingerprint(site, account)
	site.Config.ProxyConfigID, account.ProxyConfigID = &newID, &newID
	if fingerprint(site, account) != before {
		t.Fatal("backup ID remapping changed checkin identity")
	}
}

func TestAgentRouterProxyChangeRetainsLoginIntentAndSession(t *testing.T) {
	h := newHarness(t)
	var logins, confirmations atomic.Int32
	proxyA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/login" {
			requireSubmitMarker(t, h)
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "agent-proxy-session", Path: "/"})
			jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":false}}`)
			return
		}
		jsonReply(w, `{"success":true,"data":{"items":[]}}`)
	}))
	defer proxyA.Close()
	proxyB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		confirmations.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/log/self/" || r.Header.Get("Cookie") != "session=agent-proxy-session" || r.Header.Get("New-Api-User") != "42" {
			t.Error("changed proxy replayed login or lost the saved session")
		}
		jsonReply(w, agentLog(h.now.Unix()))
	}))
	defer proxyB.Close()
	node, err := op.ProxySave(context.Background(), h.s.Store.DB, 0, model.ProxyWrite{Name: "Router proxy", URL: proxyA.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	site, account := h.platformAccount("http://agent.invalid", "agentrouter", model.CheckinCredentials{Type: "password", Username: "agent", Password: "private-password"})
	bindCheckinProxy(t, h, site, node)
	h.enqueue(account, "manual")
	h.drain()
	before := h.run(account)
	if before.Status != "pending" || before.SubmitIntentAt == nil || logins.Load() != 1 {
		t.Fatal("login fixture did not retain its submission intent", before)
	}
	if _, err := op.ProxySave(context.Background(), h.s.Store.DB, node.ID, model.ProxyWrite{Name: node.Name, URL: proxyB.URL, Enabled: true, Revision: node.Revision}); err != nil {
		t.Fatal(err)
	}
	old := h.s
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	h.s = New(old.Store, old.Vault)
	h.s.Now = func() time.Time { return h.now }
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Reason != "config_changed" || confirmations.Load() != 0 {
		t.Fatal("changed proxy bypassed explicit confirmation")
	}
	if _, err := h.s.Confirm(context.Background(), before.ID, "", false); err != nil {
		t.Fatal(err)
	}
	h.drain()
	after := h.run(account)
	if (after.Status != "success" && after.Status != "already_checked_in") || after.ID != before.ID || !after.SubmitIntentAt.Equal(*before.SubmitIntentAt) || logins.Load() != 1 || confirmations.Load() != 1 || after.RewardMicros != nil {
		t.Fatal("proxy recovery replayed login or discarded its durable state", after)
	}
}

func TestSub2APIProxyChangeDoesNotReplayUncertainRefresh(t *testing.T) {
	var refreshes, nextRequests atomic.Int32
	h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("pooled checkin bypassed the proxy")
	}, false, true)
	proxyA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		if r.URL.Path != "/api/v1/auth/refresh" || r.Method != http.MethodPost {
			t.Error("unexpected mutation before token recovery")
		}
		sub2APIReply(w, 200, map[string]any{"code": 0, "data": map[string]any{"access_token": "incomplete"}})
	}))
	defer proxyA.Close()
	proxyB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextRequests.Add(1); sub2APITokens(w) }))
	defer proxyB.Close()
	node, err := op.ProxySave(context.Background(), h.s.Store.DB, 0, model.ProxyWrite{Name: "Refresh proxy", URL: proxyA.URL, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	bindCheckinProxy(t, h, site, node)
	h.enqueue(account, "manual")
	h.drain()
	before, err := h.s.Store.Account(context.Background(), account.ID)
	if err != nil || !before.RefreshPending || h.run(account).Reason != "sub2api_refresh_uncertain" {
		t.Fatal("uncertain refresh fixture failed", err)
	}
	if _, err := op.ProxySave(context.Background(), h.s.Store.DB, node.ID, model.ProxyWrite{Name: node.Name, URL: proxyB.URL, Enabled: true, Revision: node.Revision}); err != nil {
		t.Fatal(err)
	}
	old := h.s
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	h.s = New(old.Store, old.Vault)
	h.s.Now = func() time.Time { return h.now }
	h.enqueue(account, "manual")
	h.drain()
	after, err := h.s.Store.Account(context.Background(), account.ID)
	if err != nil || refreshes.Load() != 1 || nextRequests.Load() != 0 || !after.RefreshPending || after.CredentialCipher != before.CredentialCipher || h.run(account).Reason != "sub2api_refresh_uncertain" {
		t.Fatal("proxy change replayed a possibly consumed refresh token", err)
	}
}
