package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestScheduledCheckinNotificationsUsePersistedSanitizedOutcome(t *testing.T) {
	ctx := setupProjectTestDB(t)
	received := make(chan checkinNotification, 4)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid webhook request")
		}
		var event checkinNotification
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		received <- event
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	n := newCheckinNotifier(func() checkinNotificationConfig {
		return checkinNotificationConfig{enabled: true, webhook: sink.URL, cooldown: time.Hour}
	})
	previous := checkinNotifications
	checkinNotifications = n
	t.Cleanup(func() { n.wg.Wait(); checkinNotifications = previous })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid checkin-secret-value"}`))
	}))
	defer upstream.Close()
	_, account := createCheckinFixture(t, ctx, upstream.URL)
	if result, err := CheckinAccount(ctx, account.ID); err == nil || result == nil {
		t.Fatal("expected manual failure")
	}
	n.wg.Wait()
	if len(received) != 0 {
		t.Fatal("manual check-in sent a notification")
	}
	for i := 0; i < 2; i++ {
		due := time.Now().Add(-time.Minute)
		if err := db.GetDB().Model(account).Update("next_auto_checkin_at", due).Error; err != nil {
			t.Fatal(err)
		}
		result, err := checkinAccountWithTrigger(ctx, account.ID, SiteBatchTriggerScheduled)
		if err == nil || result == nil || result.LogID == 0 {
			t.Fatalf("missing scheduled failure: %+v, %v", result, err)
		}
		n.wg.Wait()
	}
	if len(received) != 1 {
		t.Fatalf("cooldown did not coalesce repeated failures: %+v", received)
	}
	event := <-received
	if event.Event != "site_checkin_failed" || event.AccountID != account.ID || event.FailureCount != 2 || strings.Contains(event.Message, account.AccessToken) {
		t.Fatalf("incorrect or unsafe failure notification: %+v", event)
	}
	var entry model.SiteCheckinLog
	if err := db.GetDB().First(&entry, event.LogID).Error; err != nil || event.Message != entry.Message || entry.Source != "scheduled" {
		t.Fatalf("notification does not reference a persisted log: %+v, %v", entry, err)
	}
}

func TestLowBalanceNotificationRequiresFreshBalance(t *testing.T) {
	events := make(chan checkinNotification, 4)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event checkinNotification
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		events <- event
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	config := checkinNotificationConfig{enabled: true, webhook: sink.URL, threshold: 1}
	n := newCheckinNotifier(func() checkinNotificationConfig { return config })
	site := &model.Site{ID: 1, Name: "Site"}
	account := &model.SiteAccount{ID: 2, Name: "Account", Balance: 0}
	result := &model.SiteCheckinResult{LogID: 3, Status: model.SiteExecutionStatusSuccess}
	n.notify(site, account, result, siteBalanceFetchResult{})
	n.notify(site, account, result, siteBalanceFetchResult{ok: true, balance: 1})
	n.wg.Wait()
	if len(events) != 0 {
		t.Fatal("stale balance or threshold equality triggered an alert")
	}
	n.notify(site, account, result, siteBalanceFetchResult{ok: true, balance: 0})
	n.wg.Wait()
	if len(events) != 1 {
		t.Fatal("a real zero balance was not reported")
	}
	event := <-events
	if event.Event != "site_checkin_low_balance" || event.Balance == nil || *event.Balance != 0 || *event.Threshold != 1 {
		t.Fatalf("incorrect balance notification: %+v", event)
	}
	config.threshold = 0
	n.notify(site, account, result, siteBalanceFetchResult{ok: true, balance: -1})
	n.wg.Wait()
	config.enabled = false
	result.Status = model.SiteExecutionStatusFailed
	n.notify(site, account, result, siteBalanceFetchResult{})
	n.wg.Wait()
	if len(events) != 0 {
		t.Fatal("disabled notifications were sent")
	}
}

func TestCheckinNotificationDeliveryIsAsyncCoalescedAndRetriable(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer sink.Close()
	defer unblock()
	n := newCheckinNotifier(nil)
	config := checkinNotificationConfig{enabled: true, webhook: sink.URL, cooldown: time.Hour}
	event := checkinNotification{AccountID: 1, Event: "site_checkin_failed", Reason: "upstream_http_error"}
	if !n.enqueue(config, event) {
		t.Fatal("first event was not queued")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	var callers sync.WaitGroup
	var duplicates atomic.Int32
	for i := 0; i < 20; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			if n.enqueue(config, event) {
				duplicates.Add(1)
			}
		}()
	}
	callers.Wait()
	if duplicates.Load() != 0 {
		t.Fatal("concurrent duplicate was queued")
	}
	unblock()
	n.wg.Wait()
	if !n.enqueue(config, event) {
		t.Fatal("failed delivery consumed the cooldown")
	}
	n.wg.Wait()
	if calls.Load() != 2 || n.enqueue(config, event) {
		t.Fatal("successful delivery did not start cooldown")
	}
	// Expired entries are pruned; no timer or unbounded retry loop is needed.
	n.mu.Lock()
	for key := range n.recent {
		n.recent[key] = checkinNotificationReservation{until: time.Now().Add(-time.Second)}
	}
	n.mu.Unlock()
	if !n.enqueue(config, event) {
		t.Fatal("expired cooldown blocked delivery")
	}
	n.wg.Wait()
}

func TestCheckinNotificationDoesNotFollowRedirectsAndTimesOut(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	n := newCheckinNotifier(nil)
	if err := n.deliver(redirect.URL, checkinNotification{}); err == nil || redirected.Load() != 0 {
		t.Fatal("webhook followed a redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-ctx.Done():
		}
	}))
	defer slow.Close()
	defer cancel()
	n.client.Timeout = 20 * time.Millisecond
	if err := n.deliver(slow.URL, checkinNotification{}); err == nil {
		t.Fatal("slow webhook did not time out")
	}
}
