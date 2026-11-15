package checkin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCleanupPreservesDailyDeduplicationAndRewards(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonReply(w, `{"success":true,"data":{"reward":2.5}}`)
	}))
	defer server.Close()
	_, account := h.account(server.URL, false, false)
	job := h.enqueue(account, "manual")
	h.drain()
	ctx := context.Background()
	before, err := h.s.Stats(ctx, "", "")
	if err != nil || before.Success != 1 {
		t.Fatal(before, err)
	}
	report, err := h.s.CleanupJobs(ctx, model.CheckinCleanupFilter{JobIDs: []string{job.ID}})
	if err != nil || report.Status != "completed" || report.Counts.Jobs != 1 || report.Counts.Attempts == 0 {
		t.Fatal(report, err)
	}
	h.enqueue(account, "manual")
	h.drain()
	run := h.run(account)
	after, err := h.s.Stats(ctx, "", "")
	if err != nil || calls.Load() != 1 || run.Status != "success" || run.DetailsCleanedAt == nil || *run.RewardMicros != 2500000 || after.Success != before.Success || after.Rewards[0].Total != before.Rewards[0].Total {
		t.Fatalf("cleanup changed check-in semantics: calls=%d run=%+v stats=%+v %v", calls.Load(), run, after, err)
	}
	status, err := h.s.CleanupStatus(ctx)
	if err != nil || status.Source != "manual" || status.Status != "completed" || status.Counts.Jobs != 1 {
		t.Fatal(status, err)
	}
}

func TestAutoCleanupDefaultsAndDailySchedule(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	create := func(days int) model.CheckinJob {
		at := h.now.AddDate(0, 0, -days)
		job := model.CheckinJob{ID: uuid.NewString(), IdempotencyKey: uuid.NewString(), Status: "completed", CreatedAt: at, UpdatedAt: at, FinishedAt: &at}
		if err := h.s.Store.DB.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
		return job
	}
	old, recent := create(91), create(89)
	if err := h.s.AutoCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Store.Job(ctx, old.ID); err != nil {
		t.Fatal("automatic cleanup enabled by default", err)
	}
	if report, _ := h.s.CleanupStatus(ctx); report != nil {
		t.Fatal("disabled cleanup wrote a report")
	}
	settings, _ := h.s.Store.Settings(ctx)
	if settings.AutoCleanup || settings.RetentionDays != 90 {
		t.Fatal("wrong retention defaults", settings)
	}
	settings.AutoCleanup = true
	if err := h.s.Store.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err := h.s.AutoCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Store.Job(ctx, old.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("old task retained", err)
	}
	if _, _, err := h.s.Store.Job(ctx, recent.ID); err != nil {
		t.Fatal("recent task removed", err)
	}
	old = create(100)
	h.now = h.now.Add(23 * time.Hour)
	if err := h.s.AutoCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Store.Job(ctx, old.ID); err != nil {
		t.Fatal("automatic cleanup ran twice within a day", err)
	}
	h.now = h.now.Add(time.Hour)
	if err := h.s.AutoCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Store.Job(ctx, old.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("next-day cleanup did not run", err)
	}
	status, err := h.s.CleanupStatus(ctx)
	if err != nil || status.Source != "automatic" || status.Counts.Jobs != 1 {
		t.Fatal(status, err)
	}
	// Changing retention does not bypass the daily rate limit.
	settings.RetentionDays = 30
	if err := h.s.Store.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err := h.s.AutoCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Store.Job(ctx, recent.ID); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(24 * time.Hour)
	if err := h.s.AutoCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.s.Store.Job(ctx, recent.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("new retention setting was ignored", err)
	}
}

func TestCleanupReportsPartialFailureAndSerializesRuns(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ids := []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"}
	for _, id := range ids {
		job := model.CheckinJob{ID: id, IdempotencyKey: uuid.NewString(), Status: "completed"}
		if err := h.s.Store.DB.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := h.s.Store.DB.Callback().Delete().Before("gorm:delete").Register("test:partial-cleanup", func(tx *gorm.DB) {
		if job, ok := tx.Statement.Dest.(*model.CheckinJob); ok && job.ID == ids[1] {
			_ = tx.AddError(errors.New("simulated private SQL error"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	filter := model.CheckinCleanupFilter{JobIDs: ids}
	h.s.cleanupMu.Lock()
	_, err := h.s.CleanupJobs(ctx, filter)
	h.s.cleanupMu.Unlock()
	if !errors.Is(err, op.ErrCheckinConflict) {
		t.Fatal("overlapping cleanup accepted", err)
	}
	report, err := h.s.CleanupJobs(ctx, filter)
	if err != nil || report.Status != "partial" || report.Counts.Jobs != 1 || report.FinishedAt == nil {
		t.Fatal(report, err)
	}
	if _, _, err := h.s.Store.Job(ctx, ids[1]); err != nil {
		t.Fatal("failed task deleted", err)
	}
	status, err := h.s.CleanupStatus(ctx)
	if err != nil || status.Status != "partial" || status.Counts.Jobs != 1 {
		t.Fatal(status, err)
	}
	// An orphaned report after restart is never presented as an active cleanup.
	report.Status, report.FinishedAt = "running", nil
	if err := h.s.Store.SaveCleanupReport(ctx, *report); err != nil {
		t.Fatal(err)
	}
	if status, err := h.s.CleanupStatus(ctx); err != nil || status.Status != "interrupted" {
		t.Fatal(status, err)
	}
}
