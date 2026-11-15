package checkin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type harness struct {
	t   *testing.T
	s   *Service
	now time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	conn, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "checkin.db"))+"?_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(&model.CheckinSite{}, &model.CheckinAccount{}, &model.CheckinRun{}, &model.CheckinAttempt{}, &model.CheckinJob{}, &model.CheckinJobItem{}, &model.CheckinNotificationTarget{}, &model.CheckinNotificationDelivery{}, &model.CheckinTurnstileSettings{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	sqlDB.SetMaxOpenConns(8)
	vault, err := checkinsecret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, s: New(op.NewCheckinStore(conn), vault), now: time.Date(2030, 11, 14, 12, 0, 0, 0, time.UTC)}
	h.s.Now = func() time.Time { return h.now }
	settings := model.DefaultCheckinSettings()
	settings.SiteIntervalSeconds, settings.ConfirmAttempts, settings.HTTPTimeoutSeconds = 0, 1, 2
	if err := h.s.Store.SaveSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.s.Close(); _ = sqlDB.Close() })
	return h
}

func (h *harness) account(base string, status, auto bool) (model.CheckinSite, model.CheckinAccount) {
	h.t.Helper()
	config := model.CheckinSiteConfig{Name: "Independent site", BaseURL: base, Adapter: "http", Enabled: true, Timezone: "UTC", WindowStart: "00:00", WindowEnd: "24:00", Submit: model.CheckinRequest{Method: "POST", Path: "/submit"}, Reward: model.CheckinRewardRule{Source: "submit", Path: "data.reward", Unit: "points", Multiplier: "1"}}
	if status {
		config.Status = &model.CheckinStatusRule{Request: model.CheckinRequest{Method: "GET", Path: "/status?date={{checkin_date}}"}, Checked: model.CheckinMatch{Path: "data.checked_in", Value: json.RawMessage("true")}, Unchecked: model.CheckinMatch{Path: "data.checked_in", Value: json.RawMessage("false")}, DatePath: "data.date"}
		config.Reward.Source = "status"
	}
	site, err := h.s.Store.SaveSite(context.Background(), "", 0, config)
	if err != nil {
		h.t.Fatal(err)
	}
	name := "Standalone account"
	account, err := h.s.SaveAccount(context.Background(), site.ID, "", AccountWrite{Name: &name, AutoCheckin: &auto, Credentials: &model.CheckinCredentials{Type: "cookie", Cookie: "session=checkin-secret", UserID: "42"}})
	if err != nil {
		h.t.Fatal(err)
	}
	return site, account
}

func (h *harness) enqueue(account model.CheckinAccount, source string) model.CheckinJob {
	h.t.Helper()
	job, err := h.s.Enqueue(context.Background(), model.CheckinScope{Kind: "accounts", IDs: []string{account.ID}}, source, "")
	if err != nil {
		h.t.Fatal(err)
	}
	return job
}

func (h *harness) drain() {
	h.t.Helper()
	for i := 0; i < 30; i++ {
		if !h.s.ProcessOne(context.Background()) {
			return
		}
	}
	h.t.Fatal("queue did not drain")
}

func (h *harness) run(account model.CheckinAccount) model.CheckinRun {
	h.t.Helper()
	runs, err := h.s.Store.Runs(context.Background(), op.CheckinRunFilter{AccountID: account.ID})
	if err != nil || len(runs) != 1 {
		h.t.Fatalf("expected one daily ledger, got %d: %v", len(runs), err)
	}
	return runs[0]
}

func jsonReply(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestIndependentCheckinAndConcurrentEntryPoints(t *testing.T) {
	h := newHarness(t)
	var submits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=checkin-secret" || r.Header.Get("New-Api-User") != "42" {
			t.Error("dedicated credentials not used")
		}
		var markers int64
		h.s.Store.DB.Model(&model.CheckinRun{}).Where("submit_intent_at IS NOT NULL").Count(&markers)
		if markers != 1 {
			t.Error("request preceded durable submit marker")
		}
		submits.Add(1)
		jsonReply(w, `{"success":true,"data":{"reward":0},"message":"session=checkin-secret"}`)
	}))
	defer server.Close()
	site, account := h.account(server.URL, false, true)
	for _, unrelated := range []string{"channels", "channel_keys", "sites", "site_accounts"} {
		if h.s.Store.DB.Migrator().HasTable(unrelated) {
			t.Fatalf("unexpected dependency on %s", unrelated)
		}
	}
	if account.CredentialUserID != "42" {
		t.Fatal("public user ID missing")
	}
	raw, _ := json.Marshal(account)
	if strings.Contains(string(raw), "checkin-secret") || strings.Contains(string(raw), "credential_cipher") {
		t.Fatal("credential exposed")
	}
	if strings.Contains(account.CredentialCipher, "checkin-secret") {
		t.Fatal("credential stored in plaintext")
	}

	var wg sync.WaitGroup
	jobIDs := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			job, err := h.s.Enqueue(context.Background(), model.CheckinScope{Kind: "accounts", IDs: []string{account.ID}}, "manual", "stable-request")
			if err != nil {
				t.Error(err)
				return
			}
			jobIDs <- job.ID
		})
	}
	wg.Wait()
	close(jobIDs)
	first := ""
	for id := range jobIDs {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("same idempotency key created multiple tasks")
		}
	}
	_, err := h.s.Enqueue(context.Background(), model.CheckinScope{Kind: "sites", IDs: []string{site.ID}}, "manual", "stable-request")
	if !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatalf("key scope mismatch accepted: %v", err)
	}
	_, err = h.s.Enqueue(context.Background(), model.CheckinScope{Kind: "all"}, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "scheduled")
	for i := 0; i < 8; i++ {
		wg.Go(func() { h.s.ProcessOne(context.Background()) })
	}
	wg.Wait()
	h.drain()
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	if submits.Load() != 1 || run.Status != "success" || run.RewardMicros == nil || *run.RewardMicros != 0 {
		t.Fatalf("duplicate or invalid outcome: calls=%d run=%+v", submits.Load(), run)
	}
	attempts, _ := h.s.Store.Attempts(context.Background(), run.ID)
	data, _ := json.Marshal(attempts)
	if strings.Contains(string(data), "checkin-secret") {
		t.Fatal("upstream body leaked to history")
	}
	stats, err := h.s.Stats(context.Background(), "", "")
	if err != nil || stats.Success != 1 || len(stats.Rewards) != 1 || stats.Rewards[0].Total != "0" {
		t.Fatalf("zero reward/statistics lost: %+v %v", stats, err)
	}
	jobs, _ := h.s.Store.Jobs(context.Background())
	for _, job := range jobs {
		if job.Completed != job.Total {
			t.Fatalf("incomplete task counters: %+v", job)
		}
	}
}

func TestNoHTTPRequestWhenSubmitMarkerCannotBeSaved(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); jsonReply(w, `{"success":true}`) }))
	defer server.Close()
	_, account := h.account(server.URL, false, false)
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register("test:block-marker", func(tx *gorm.DB) {
		if tx.Statement.Table == "checkin_runs" {
			if values, ok := tx.Statement.Dest.(map[string]any); ok && values["phase"] == "submit_intent" {
				_ = tx.AddError(errors.New("simulated disk failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	if calls.Load() != 0 || h.run(account).SubmitIntentAt != nil {
		t.Fatal("submission occurred without marker")
	}
	_ = h.s.Store.DB.Callback().Update().Remove("test:block-marker")
	h.now = h.now.Add(4 * time.Minute)
	h.drain()
	if calls.Load() != 1 || h.run(account).Status != "success" {
		t.Fatal("unsubmitted work did not recover")
	}
}

func TestCrashAfterSubmissionRecoversByQueryOnly(t *testing.T) {
	h := newHarness(t)
	var submits atomic.Int32
	var checked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			submits.Add(1)
			checked.Store(true)
			jsonReply(w, `{"success":true}`)
			return
		}
		jsonReply(w, fmt.Sprintf(`{"success":true,"data":{"checked_in":%t,"date":"%s","reward":"1.25"}}`, checked.Load(), r.URL.Query().Get("date")))
	}))
	defer server.Close()
	_, account := h.account(server.URL, true, false)
	_ = h.s.Store.DB.Callback().Create().Before("gorm:create").Register("test:crash-after-submit", func(tx *gorm.DB) {
		if attempt, ok := tx.Statement.Dest.(*model.CheckinAttempt); ok && attempt.Stage == "submit" {
			_ = tx.AddError(errors.New("simulated process interruption"))
		}
	})
	h.enqueue(account, "manual")
	h.drain()
	before := h.run(account)
	if submits.Load() != 1 || before.SubmitIntentAt == nil {
		t.Fatal("fixture did not submit")
	}
	_ = h.s.Store.DB.Callback().Create().Remove("test:crash-after-submit")
	h.now = h.now.Add(4 * time.Minute)
	restarted := New(h.s.Store, h.s.Vault)
	restarted.Now = func() time.Time { return h.now }
	defer restarted.Close()
	if !restarted.ProcessOne(context.Background()) {
		t.Fatal("expired lease was not recovered")
	}
	after := h.run(account)
	if submits.Load() != 1 || after.ID != before.ID || after.Status != "success" || after.RewardMicros == nil || *after.RewardMicros != 1_250_000 {
		t.Fatalf("unsafe recovery: %+v calls=%d", after, submits.Load())
	}
}

func TestAmbiguousSubmitWithNoStatusIsNeverReplayed(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	_, account := h.account(server.URL, false, true)
	h.enqueue(account, "manual")
	h.drain()
	first := h.run(account)
	h.enqueue(account, "manual")
	h.drain()
	h.now = h.now.Add(2 * time.Minute)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if calls.Load() != 1 || h.run(account).ID != first.ID || h.run(account).Status != "manual_required" {
		t.Fatal("ambiguous submission was replayed")
	}
}

func TestPrecheckPendingResumesWithoutPrematureSubmit(t *testing.T) {
	h := newHarness(t)
	var ready, checked atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			calls.Add(1)
			checked.Store(true)
			jsonReply(w, `{"success":true}`)
			return
		}
		if !ready.Load() {
			jsonReply(w, `{"success":true,"data":{"checked_in":"false","date":"2030-11-14"}}`)
			return
		}
		jsonReply(w, fmt.Sprintf(`{"data":{"checked_in":%t,"date":"%s","reward":2}}`, checked.Load(), r.URL.Query().Get("date")))
	}))
	defer server.Close()
	_, account := h.account(server.URL, true, true)
	h.enqueue(account, "scheduled")
	h.drain()
	first := h.run(account)
	if first.Status != "pending" || first.SubmitIntentAt != nil || calls.Load() != 0 {
		t.Fatal("unknown precheck submitted")
	}
	ready.Store(true)
	h.now = h.now.Add(2 * time.Minute)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if h.run(account).Status != "success" || h.run(account).ID != first.ID || calls.Load() != 1 {
		t.Fatal("precheck did not resume normal execution")
	}
}

func TestPendingConfirmationAndDeadline(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	var visible atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			calls.Add(1)
			jsonReply(w, `{"success":true}`)
			return
		}
		jsonReply(w, fmt.Sprintf(`{"data":{"checked_in":%t,"date":"%s","reward":"3.000001"}}`, visible.Load(), r.URL.Query().Get("date")))
	}))
	defer server.Close()
	_, account := h.account(server.URL, true, false)
	job := h.enqueue(account, "manual")
	h.drain()
	first := h.run(account)
	if first.Status != "pending" || first.SubmitIntentAt == nil {
		t.Fatal("confirmation should be pending")
	}
	h.now = h.now.Add(2 * time.Minute)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if calls.Load() != 1 || h.run(account).Status != "pending" {
		t.Fatal("confirmation resubmitted")
	}
	h.now = h.now.Add(time.Hour)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.run(account).Status != "manual_required" {
		t.Fatal("confirmation deadline not enforced")
	}
	visible.Store(true)
	if _, err := h.s.Confirm(context.Background(), first.ID, "", false); err != nil {
		t.Fatal(err)
	}
	h.drain()
	run := h.run(account)
	if calls.Load() != 1 || run.Status != "success" || run.RewardMicros == nil || *run.RewardMicros != 3_000_001 {
		t.Fatalf("manual confirmation failed: %+v", run)
	}
	round, _, _ := h.s.Store.Job(context.Background(), job.ID)
	if round.Pending != 1 || round.Completed != 1 {
		t.Fatal("historical task counters should describe that round")
	}
}

func TestChangedCredentialsRequireExplicitConfirmation(t *testing.T) {
	h := newHarness(t)
	var checked atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" {
			calls.Add(1)
			jsonReply(w, `{"success":true}`)
			return
		}
		jsonReply(w, fmt.Sprintf(`{"data":{"checked_in":%t,"date":"%s","reward":99}}`, checked.Load(), r.URL.Query().Get("date")))
	}))
	defer server.Close()
	_, account := h.account(server.URL, true, true)
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	updated, err := h.s.SaveAccount(context.Background(), "", account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: account.CredentialVersion, Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: "new-secret", UserID: "43"}})
	if err != nil {
		t.Fatal(err)
	}
	checked.Store(true)
	h.now = h.now.Add(2 * time.Minute)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if h.run(account).Reason != "config_changed" || calls.Load() != 1 {
		t.Fatal("changed identity was automatically trusted")
	}
	if _, err := h.s.Confirm(context.Background(), run.ID, "", false); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if h.run(account).Status != "already_checked_in" || h.run(account).RewardMicros != nil || updated.CredentialUserID != "43" {
		t.Fatal("identity change claimed a new reward")
	}
}

func TestDisabledEntriesAndCancellation(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); jsonReply(w, `{"success":true}`) }))
	defer server.Close()
	_, account := h.account(server.URL, false, true)
	job := h.enqueue(account, "manual")
	disabled := false
	if _, err := h.s.SaveAccount(context.Background(), "", account.ID, AccountWrite{Revision: account.Revision, Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	h.drain()
	result, _, _ := h.s.Store.Job(context.Background(), job.ID)
	if calls.Load() != 0 || result.Skipped != 1 || result.Completed != 1 {
		t.Fatalf("disabled account executed: %+v", result)
	}

	_, other := h.account(server.URL, false, true)
	canceled := h.enqueue(other, "manual")
	if err := h.s.Store.CancelJob(context.Background(), canceled.ID); err != nil {
		t.Fatal(err)
	}
	h.drain()
	result, _, _ = h.s.Store.Job(context.Background(), canceled.ID)
	if result.Status != "canceled" || result.Completed != 1 || result.Skipped != 1 || calls.Load() != 0 {
		t.Fatalf("cancel not reconciled: %+v", result)
	}
}

func TestCanceledExpiredLeaseDoesNotBlockFutureJobs(t *testing.T) {
	h := newHarness(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { jsonReply(w, `{"success":true}`) }))
	defer server.Close()
	_, account := h.account(server.URL, false, false)
	job := h.enqueue(account, "manual")
	items, err := h.s.Store.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := h.s.Store.Claim(context.Background(), &items[0], h.now)
	if err != nil || !claimed {
		t.Fatal("claim failed", err)
	}
	if err := h.s.Store.CancelJob(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(4 * time.Minute)
	if err := h.s.Store.Recover(context.Background(), h.now); err != nil {
		t.Fatal(err)
	}
	busy, _ := h.s.Store.HasQueuedAccount(context.Background(), account.ID)
	if busy {
		t.Fatal("canceled expired item still blocks account")
	}
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Status != "success" {
		t.Fatal("future job blocked")
	}
}

func TestLeaseProtectsCredentialAndSiteEdits(t *testing.T) {
	h := newHarness(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		jsonReply(w, `{"success":true}`)
	}))
	defer server.Close()
	site, account := h.account(server.URL, false, false)
	h.enqueue(account, "manual")
	finished := make(chan struct{})
	go func() { h.s.ProcessOne(context.Background()); close(finished) }()
	<-entered
	name := "Changed"
	_, accountErr := h.s.SaveAccount(context.Background(), "", account.ID, AccountWrite{Revision: account.Revision, Name: &name})
	_, siteErr := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config)
	deleteErr := h.s.Store.Delete(context.Background(), "", account.ID)
	close(release)
	<-finished
	for _, err := range []error{accountErr, siteErr, deleteErr} {
		if !errors.Is(err, op.ErrCheckinConflict) {
			t.Fatalf("active lease did not reject edit: %v", err)
		}
	}
	_, err := h.s.SaveAccount(context.Background(), "", account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: 0, Credentials: &model.CheckinCredentials{Type: "cookie", Cookie: "new"}})
	if !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatal("stale credential version accepted")
	}
}

func TestUnsupportedSiteIsSkippedUntilConfigurationChanges(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	var supported atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !supported.Load() {
			http.NotFound(w, r)
			return
		}
		jsonReply(w, `{"success":true}`)
	}))
	defer server.Close()
	site, account := h.account(server.URL, false, true)
	h.enqueue(account, "manual")
	h.drain()
	site, _ = h.s.Store.Site(context.Background(), site.ID)
	if site.Support != "unsupported" || !h.run(account).SafeToRetry {
		t.Fatal("404 capability evidence missing")
	}
	h.now = h.now.Add(20 * time.Minute)
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if calls.Load() != 1 {
		t.Fatal("unsupported site was repeatedly scheduled")
	}
	supported.Store(true)
	site.Config.Submit.Path = "/fixed"
	site, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config)
	if err != nil {
		t.Fatal(err)
	}
	if site.Support != "unknown" {
		t.Fatal("capability did not reset after configuration change")
	}
	h.enqueue(account, "manual")
	h.drain()
	if calls.Load() != 2 || h.run(account).Status != "success" {
		t.Fatal("explicitly rejected submission could not be repaired")
	}
}
