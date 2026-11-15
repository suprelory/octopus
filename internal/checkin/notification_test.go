package checkin

import (
	"context"
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
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func notificationConfig() model.CheckinNotificationConfig {
	return model.CheckinNotificationConfig{Name: "Test destination", Kind: "webhook", Enabled: true, Events: []string{"success", "already_checked_in", "failed", "attention"}, IncludeManual: true, Scope: "all", Language: "en", ProxyMode: "direct"}
}

func (h *harness) notification(url string, config model.CheckinNotificationConfig) model.CheckinNotificationTarget {
	h.t.Helper()
	target, err := h.s.SaveNotification(context.Background(), "", NotificationWrite{Config: config, Connection: &notify.Connection{URL: url}})
	if err != nil {
		h.t.Fatal(err)
	}
	return target
}

func (h *harness) deliveries() []model.CheckinNotificationDelivery {
	h.t.Helper()
	rows, err := h.s.Store.NotificationDeliveries(context.Background(), op.NotificationDeliveryFilter{Limit: 100})
	if err != nil {
		h.t.Fatal(err)
	}
	return rows
}

func TestNotificationsFollowSavedResultsWithoutRepeatingCheckins(t *testing.T) {
	h := newHarness(t)
	var submits, good, bad atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submits.Add(1)
		jsonReply(w, `{"success":true,"data":{"reward":0},"message":"private-upstream-message"}`)
	}))
	defer upstream.Close()
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["event"] != "success" || strings.Contains(payload["message"], "private-") || strings.Contains(payload["message"], "checkin-secret") {
			t.Error("notification payload exposed upstream data")
		}
		if r.URL.Path == "/good" {
			good.Add(1)
			w.WriteHeader(204)
		} else {
			bad.Add(1)
			w.WriteHeader(502)
		}
	}))
	defer sink.Close()
	h.notification(sink.URL+"/good", notificationConfig())
	h.notification(sink.URL+"/bad", notificationConfig())
	_, account := h.account(upstream.URL, false, false)
	h.enqueue(account, "manual")
	h.drain()
	if rows := h.deliveries(); len(rows) != 2 || good.Load()+bad.Load() != 0 || h.run(account).Status != "success" {
		t.Fatal("checkin depended on notification delivery")
	}
	h.enqueue(account, "manual")
	h.drain()
	for h.s.ProcessNotification(context.Background()) {
	}
	rows := h.deliveries()
	states := map[string]int{}
	for _, row := range rows {
		states[row.Status]++
	}
	if len(rows) != 2 || states["sent"] != 1 || states["uncertain"] != 1 || good.Load() != 1 || bad.Load() != 1 || submits.Load() != 1 || h.run(account).Status != "success" {
		t.Fatal("notification failure changed checkin state or repeated a delivery", states)
	}
}

func TestNotificationEnqueueFailureCannotRollBackCheckin(t *testing.T) {
	h := newHarness(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { jsonReply(w, `{"success":true}`) }))
	defer upstream.Close()
	h.notification("https://example.invalid/never-sent", notificationConfig())
	_, account := h.account(upstream.URL, false, false)
	if err := h.s.Store.DB.Callback().Create().Before("gorm:create").Register("test:block-notification", func(tx *gorm.DB) {
		if tx.Statement.Table == "checkin_notification_deliveries" {
			tx.AddError(errors.New("unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.s.Store.DB.Callback().Create().Remove("test:block-notification") })
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" || len(h.deliveries()) != 0 {
		t.Fatal("notification queue failure affected the saved result")
	}
}

func TestNotificationSecretsPreservationRevisionAndStaleQueue(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	config := notificationConfig()
	config.Enabled = false
	target := h.notification("https://example.invalid/private-url-token", config)
	public, _ := json.Marshal(target)
	if !target.HasConnection || target.ConnectionCipher == "" || strings.Contains(string(public), "private-url-token") || strings.Contains(string(public), "connection_cipher") || strings.Contains(target.ConnectionCipher, "private-url-token") {
		t.Fatal("notification connection was exposed")
	}
	if _, err := h.s.Vault.Unseal(target.ID, target.ConnectionCipher); err == nil {
		t.Fatal("notification cipher did not use isolated AAD")
	}
	test, err := h.s.Store.TestNotification(ctx, target.ID, target.Revision, h.now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.s.Store.TestNotification(ctx, target.ID, target.Revision, h.now)
	if err != nil || test.ID != second.ID {
		t.Fatal("double-clicking test queued duplicate messages")
	}
	config.Name = "Renamed"
	updated, err := h.s.SaveNotification(ctx, target.ID, NotificationWrite{Config: config, Revision: target.Revision})
	if err != nil || updated.ConnectionCipher != target.ConnectionCipher || updated.ConnectionVersion != target.ConnectionVersion {
		t.Fatal("metadata edit lost or replaced the connection", err)
	}
	if rows := h.deliveries(); len(rows) != 1 || rows[0].Status != "skipped" || rows[0].Reason != "config_changed" {
		t.Fatal("stale queued test survived configuration edit")
	}
	if _, err := h.s.SaveNotification(ctx, target.ID, NotificationWrite{Config: config, Revision: target.Revision}); !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatal("stale edit was accepted", err)
	}
	config.Kind = "bark"
	if _, err := h.s.SaveNotification(ctx, target.ID, NotificationWrite{Config: config, Revision: updated.Revision}); err == nil {
		t.Fatal("platform changed without a replacement connection")
	}
	config.Kind = "webhook"
	config.Scope = "sites"
	if _, err := h.s.SaveNotification(ctx, target.ID, NotificationWrite{Config: config, Revision: updated.Revision}); err == nil {
		t.Fatal("empty selected scope silently widened")
	}
}

func TestNotificationBalanceUsesFreshKnownMatchingUnit(t *testing.T) {
	h := newHarness(t)
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/auth/me" {
			t.Error("notification caused an extra upstream operation")
		}
		switch mode.Load() {
		case 0:
			jsonReply(w, `{"code":0,"data":{"id":42,"balance":0}}`)
		case 1:
			w.WriteHeader(401)
		case 2:
			jsonReply(w, `{"code":0,"data":{"id":42}}`)
		case 3:
			jsonReply(w, `{"code":0,"data":{"id":42,"balance":-0.000001}}`)
		case 4:
			jsonReply(w, `{"code":0,"data":{"id":42,"balance":1}}`)
		}
	}))
	defer server.Close()
	threshold := int64(1000000)
	config := notificationConfig()
	config.Events, config.ThresholdMicros, config.ThresholdUnit = []string{"low_balance"}, &threshold, "USD"
	h.notification("https://example.invalid/usd", config)
	config.ThresholdUnit = "quota"
	h.notification("https://example.invalid/quota", config)
	config.ThresholdUnit, config.IncludeManual = "USD", false
	h.notification("https://example.invalid/automatic", config)
	site, account := h.platformAccount(server.URL, "sub2api", model.CheckinCredentials{Type: "bearer", AccessToken: "private-token", UserID: "42"})
	for i, expected := range []int{1, 1, 1, 2, 2} {
		mode.Store(int32(i))
		result := inspectAccount(t, h, account, "balance")
		h.s.notifyBalance(context.Background(), site, result) // Duplicate completion is deduplicated.
		if rows := h.deliveries(); len(rows) != expected {
			t.Fatalf("balance mode %d produced %d events, want %d", i, len(rows), expected)
		}
	}
	rows := h.deliveries()
	for _, row := range rows {
		if row.Event.BalanceMicros == nil || *row.Event.BalanceMicros > 0 || row.Event.BalanceUnit != "USD" {
			t.Fatal("unknown balance became zero or units were mixed")
		}
	}
}

func TestNotificationCooldownScopeAndCurrentConfirmationSource(t *testing.T) {
	h := newHarness(t)
	config := notificationConfig()
	config.CooldownSeconds, config.IncludeManual = 60, false
	target := h.notification("https://example.invalid/first", config)
	event := model.CheckinNotificationEvent{Kind: "failed", Identity: uuid.NewString(), SiteID: uuid.NewString(), AccountID: uuid.NewString(), SiteName: "site", AccountName: "account", Status: "failed", Source: "scheduled", CheckinDate: "2030-11-14", Timezone: "UTC", OccurredAt: h.now}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := h.s.Store.EnqueueNotifications(ctx, event, h.now); err != nil {
			t.Fatal(err)
		}
	}
	event.Identity = uuid.NewString()
	_ = h.s.Store.EnqueueNotifications(ctx, event, h.now)
	event.Identity, event.Kind, event.Status = uuid.NewString(), "success", "success"
	_ = h.s.Store.EnqueueNotifications(ctx, event, h.now)
	event.Identity, event.AccountID, event.Kind, event.Status = uuid.NewString(), uuid.NewString(), "failed", "failed"
	_ = h.s.Store.EnqueueNotifications(ctx, event, h.now)
	rows := h.deliveries()
	queued, skipped := 0, 0
	for _, row := range rows {
		if row.Status == "queued" {
			queued++
		}
		if row.Reason == "cooldown" {
			skipped++
		}
	}
	if len(rows) != 4 || queued != 3 || skipped != 1 {
		t.Fatal("cooldown was not isolated by account/event or deduplication failed")
	}
	updated, err := h.s.SaveNotification(ctx, target.ID, NotificationWrite{Revision: target.Revision, Config: config, Connection: &notify.Connection{URL: "https://example.invalid/new-destination"}})
	if err != nil || updated.ConnectionVersion == target.ConnectionVersion {
		t.Fatal("connection change retained the old cooldown", err)
	}
	event.Identity = uuid.NewString()
	_ = h.s.Store.EnqueueNotifications(ctx, event, h.now)
	rows = h.deliveries()
	queued = 0
	for _, row := range rows {
		if row.Status == "queued" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatal("new destination inherited cooldown or old queue")
	}
	// A run initially started manually may later be confirmed automatically.
	run := model.CheckinRun{ID: uuid.NewString(), SiteID: event.SiteID, AccountID: uuid.NewString(), Status: "success", Source: "manual", CheckinDate: "2030-11-14", Timezone: "UTC"}
	h.s.notifyRun(ctx, run, "confirmation_auto")
	if len(h.deliveries()) != 6 {
		t.Fatal("notification used the original run source instead of the current job")
	}
	run.ID = uuid.NewString()
	h.s.notifyRun(ctx, run, "confirmation")
	if len(h.deliveries()) != 6 {
		t.Fatal("manual notifications ignored the target's manual-event switch")
	}
}

func TestNotificationRateLimitSurvivesRestartAndManualRetryIsAtomic(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	var accept atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if accept.Load() {
			w.WriteHeader(204)
		} else {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
		}
	}))
	defer sink.Close()
	config := notificationConfig()
	config.Enabled = false
	target := h.notification(sink.URL, config)
	ctx := context.Background()
	row, err := h.s.Store.TestNotification(ctx, target.ID, target.Revision, h.now)
	if err != nil {
		t.Fatal(err)
	}
	if !h.s.ProcessNotification(ctx) || h.s.ProcessNotification(ctx) {
		t.Fatal("rate limit was not delayed")
	}
	restarted := New(op.NewCheckinStore(h.s.Store.DB), h.s.Vault)
	restarted.Now = h.s.Now
	defer restarted.Close()
	for i := 0; i < 2; i++ {
		h.now = h.now.Add(time.Second)
		if !restarted.ProcessNotification(ctx) {
			t.Fatal("durable retry was lost on restart")
		}
	}
	rows := h.deliveries()
	if len(rows) != 1 || rows[0].Status != "failed" || rows[0].Attempts != 3 || rows[0].Reason != "rate_limit_exhausted" || calls.Load() != 3 || restarted.ProcessNotification(ctx) {
		t.Fatal("rate limit retries were not bounded")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			if _, err := h.s.Store.RetryNotification(ctx, row.ID, 3, target.Revision, h.now); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, op.ErrCheckinConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("concurrent retry accepted multiple mutations")
	}
	accept.Store(true)
	if !restarted.ProcessNotification(ctx) || h.deliveries()[0].Status != "sent" || calls.Load() != 4 {
		t.Fatal("explicit retry failed")
	}
}

func TestNotificationSendingLeaseBlocksEditsAndRecoversWithoutReplay(t *testing.T) {
	h := newHarness(t)
	target := h.notification("https://example.invalid/never-send", notificationConfig())
	ctx := context.Background()
	_, _ = h.s.Store.TestNotification(ctx, target.ID, target.Revision, h.now)
	_, _, ok, err := h.s.Store.ClaimNotification(ctx, h.now)
	if err != nil || !ok {
		t.Fatal("notification was not claimed", err)
	}
	if _, err := h.s.SaveNotification(ctx, target.ID, NotificationWrite{Config: target.Config, Revision: target.Revision}); !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatal("changed an in-flight destination", err)
	}
	if err := h.s.Store.DeleteNotificationTarget(ctx, target.ID, target.Revision, h.now); !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatal("deleted an in-flight destination", err)
	}
	h.now = h.now.Add(46 * time.Second)
	if h.s.ProcessNotification(ctx) {
		t.Fatal("interrupted delivery was replayed")
	}
	rows := h.deliveries()
	if len(rows) != 1 || rows[0].Status != "uncertain" || rows[0].Reason != "delivery_interrupted" {
		t.Fatal("interrupted delivery lost its uncertainty")
	}
}

func TestNotificationIncludesAutomaticConfirmationExpiry(t *testing.T) {
	h := newHarness(t)
	config := notificationConfig()
	config.Events, config.IncludeManual = []string{"attention"}, false
	h.notification("https://example.invalid/never-send", config)
	site, account := h.account("https://example.invalid", false, false)
	expired := h.now.Add(-time.Minute)
	run := model.CheckinRun{ID: uuid.NewString(), SiteID: site.ID, AccountID: account.ID, SiteName: site.Config.Name, AccountName: account.Name, CheckinDate: "2030-11-14", Timezone: "UTC", Status: "pending", Source: "manual", ConfirmDeadline: &expired, SubmitIntentAt: &expired}
	if err := h.s.Store.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows := h.deliveries()
	if len(rows) != 1 || rows[0].Event.Status != "manual_required" || rows[0].Event.Source != "confirmation_auto" {
		t.Fatal("automatic expiry did not emit an attention event")
	}
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.deliveries()) != 1 {
		t.Fatal("expiry emitted repeated notifications")
	}
}

func TestNotificationBusyTargetDoesNotBlockAnotherDestination(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	first := h.notification("https://example.invalid/first", notificationConfig())
	if _, err := h.s.Store.TestNotification(ctx, first.ID, first.Revision, h.now); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := h.s.Store.ClaimNotification(ctx, h.now); !ok || err != nil {
		t.Fatal("first target was not claimed", err)
	}
	h.now = h.now.Add(time.Second)
	event := model.CheckinNotificationEvent{Kind: "failed", Status: "failed", Source: "manual", Identity: uuid.NewString(), SiteID: uuid.NewString(), AccountID: uuid.NewString(), SiteName: "Site", AccountName: "Account", CheckinDate: "2030-11-14", Timezone: "UTC", OccurredAt: h.now}
	if err := h.s.Store.EnqueueNotifications(ctx, event, h.now); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Second)
	second := h.notification("https://example.invalid/second", notificationConfig())
	if _, err := h.s.Store.TestNotification(ctx, second.ID, second.Revision, h.now); err != nil {
		t.Fatal(err)
	}
	row, target, ok, err := h.s.Store.ClaimNotification(ctx, h.now)
	if err != nil || !ok || row.TargetID != second.ID || target.ID != second.ID {
		t.Fatal("a busy target blocked a different destination", err)
	}
}

func TestNotificationWorkerUsesBackupGateAndSeparateAccountCapacity(t *testing.T) {
	h := newHarness(t)
	entered, release := make(chan struct{}), make(chan struct{})
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) }))
	defer sink.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	target := h.notification(sink.URL, notificationConfig())
	ctx := context.Background()
	_, _ = h.s.Store.TestNotification(ctx, target.ID, target.Revision, h.now)
	sent := make(chan struct{})
	go func() { defer close(sent); h.s.ProcessNotification(ctx) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("notification did not start")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { jsonReply(w, `{"success":true}`) }))
	defer upstream.Close()
	_, account := h.account(upstream.URL, false, false)
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" {
		t.Fatal("notification blocked account execution")
	}
	exclusive := make(chan struct{})
	go func() { _ = h.s.Exclusive(ctx, func() error { close(exclusive); return nil }) }()
	select {
	case <-exclusive:
		t.Fatal("backup overlapped active delivery")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-sent:
	case <-time.After(3 * time.Second):
		t.Fatal("sender did not stop")
	}
	select {
	case <-exclusive:
	case <-time.After(3 * time.Second):
		t.Fatal("backup gate was not released")
	}
}
