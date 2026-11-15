package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func completedCleanupFixture(t *testing.T) (*CheckinStore, *model.CheckinDump) {
	t.Helper()
	store, dump := checkinBackupFixture(t)
	var item model.CheckinJobItem
	if err := store.DB.First(&item, "id = ?", dump.Items[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	run := dump.Runs[0]
	reward := int64(1234567)
	run.Status, run.RewardState, run.RewardUnit, run.RewardMicros = "success", "known", "points", &reward
	if err := store.Complete(context.Background(), item, &run, "success", nil); err != nil {
		t.Fatal(err)
	}
	dump, err := exportCheckin(context.Background(), store.DB)
	if err != nil {
		t.Fatal(err)
	}
	return store, dump
}

func TestCheckinCleanupPreservesSharedLedgerAndBackup(t *testing.T) {
	store, dump := completedCleanupFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	job, item, attempt := dump.Jobs[0], dump.Items[0], dump.Attempts[0]
	job.ID, job.IdempotencyKey = uuid.NewString(), uuid.NewString()
	item.ID, item.JobID = uuid.NewString(), job.ID
	attempt.ID, attempt.JobID = uuid.NewString(), job.ID
	for _, row := range []any{&job, &item, &attempt} {
		if err := store.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	f := model.CheckinCleanupFilter{JobIDs: []string{dump.Jobs[0].ID, dump.Jobs[0].ID}}
	preview, err := store.PreviewCleanup(ctx, f, now)
	if err != nil || preview.Matched != 1 || preview.Jobs != 1 || preview.Items != 1 || preview.Attempts != 1 {
		t.Fatalf("unexpected preview: %+v %v", preview, err)
	}
	if _, _, err := store.Job(ctx, dump.Jobs[0].ID); err != nil {
		t.Fatal("preview removed the job", err)
	}
	run, _ := store.Run(ctx, dump.Runs[0].ID)
	if run.DetailsCleanedAt != nil {
		t.Fatal("preview changed the ledger")
	}
	counts, err := store.CleanupJobs(ctx, f, now)
	if err != nil || counts.Jobs != 1 || counts.Items != 1 || counts.Attempts != 1 {
		t.Fatalf("cleanup failed: %+v %v", counts, err)
	}
	run, err = store.Run(ctx, run.ID)
	if err != nil || run.DetailsCleanedAt == nil || !run.DetailsCleanedAt.Equal(now) || !run.UpdatedAt.Equal(dump.Runs[0].UpdatedAt) || run.Status != "success" || *run.RewardMicros != 1234567 {
		t.Fatalf("ledger changed: %+v %v", run, err)
	}
	account, _ := store.Account(ctx, run.AccountID)
	if account.LastRunID != run.ID || account.CredentialCipher != dump.Accounts[0].Cipher {
		t.Fatal("account changed during cleanup")
	}
	attempts, err := store.Attempts(ctx, run.ID)
	if err != nil || len(attempts) != 1 || attempts[0].JobID != job.ID {
		t.Fatal("cleanup removed another job's diagnostic steps", attempts, err)
	}
	exported, err := exportCheckin(ctx, store.DB)
	if err != nil || len(exported.Jobs) != 1 || len(exported.Items) != 1 || len(exported.Runs) != 1 {
		t.Fatal("invalid cleaned backup", err)
	}
	target := checkinTestDB(t)
	restoreCheckinFixture(t, target, exported)
	restored, err := NewCheckinStore(target).Run(ctx, run.ID)
	if err != nil || restored.DetailsCleanedAt == nil || restored.Status != "success" || *restored.RewardMicros != *run.RewardMicros {
		t.Fatal("cleanup marker or reward missing from restored backup", err)
	}
	if _, err := store.CleanupJobs(ctx, model.CheckinCleanupFilter{JobIDs: []string{job.ID}}, now); err != nil {
		t.Fatal(err)
	}
	exported, err = exportCheckin(ctx, store.DB)
	if err != nil || len(exported.Jobs) != 0 || len(exported.Items) != 0 || len(exported.Attempts) != 0 || len(exported.Runs) != 1 {
		t.Fatal("all-details cleanup damaged the ledger", err)
	}
	restoreCheckinFixture(t, checkinTestDB(t), exported)
	counts, err = store.CleanupJobs(ctx, f, now)
	if err != nil || counts.Jobs != 0 || counts.Skipped["missing"] != 1 {
		t.Fatal("repeat cleanup was not safe", counts, err)
	}
}

func TestCheckinCleanupProtectsUnfinishedWork(t *testing.T) {
	for _, test := range []struct {
		name, jobStatus, itemState, runStatus, lease, reason string
		confirm, shared                                      bool
	}{
		{name: "queued", jobStatus: "queued", reason: "active"},
		{name: "running", jobStatus: "running", reason: "active"},
		{name: "canceled with active request", jobStatus: "canceled", itemState: "running", reason: "active"},
		{name: "pending", runStatus: "pending", reason: "unconfirmed"},
		{name: "manual attention", runStatus: "manual_required", reason: "unconfirmed"},
		{name: "running ledger", runStatus: "running", reason: "unconfirmed"},
		{name: "scheduled confirmation", confirm: true, reason: "unconfirmed"},
		{name: "account inspection lease", lease: "account", reason: "leased"},
		{name: "item lease", lease: "item", reason: "leased"},
		{name: "other queued confirmation", shared: true, reason: "active"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, dump := completedCleanupFixture(t)
			now := time.Now().UTC()
			update := func(row any, id string, values map[string]any) {
				t.Helper()
				if err := store.DB.Model(row).Where("id = ?", id).Updates(values).Error; err != nil {
					t.Fatal(err)
				}
			}
			if test.jobStatus != "" {
				update(&model.CheckinJob{}, dump.Jobs[0].ID, map[string]any{"status": test.jobStatus})
			}
			if test.itemState != "" {
				update(&model.CheckinJobItem{}, dump.Items[0].ID, map[string]any{"state": test.itemState})
			}
			if test.runStatus != "" {
				update(&model.CheckinRun{}, dump.Runs[0].ID, map[string]any{"status": test.runStatus})
			}
			if test.confirm {
				update(&model.CheckinRun{}, dump.Runs[0].ID, map[string]any{"next_confirm_at": now.Add(time.Hour)})
			}
			if test.lease == "account" {
				update(&model.CheckinAccount{}, dump.Accounts[0].ID, map[string]any{"lease_until": now.Add(time.Hour)})
			} else if test.lease == "item" {
				update(&model.CheckinJobItem{}, dump.Items[0].ID, map[string]any{"lease_until": now.Add(time.Hour)})
			}
			if test.shared {
				job, item := dump.Jobs[0], dump.Items[0]
				job.ID, job.IdempotencyKey, job.Status = uuid.NewString(), uuid.NewString(), "queued"
				item.ID, item.JobID, item.State, item.RunID = uuid.NewString(), job.ID, "queued", ""
				if err := store.DB.Create(&job).Error; err != nil {
					t.Fatal(err)
				}
				if err := store.DB.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
			}
			filter := model.CheckinCleanupFilter{JobIDs: []string{dump.Jobs[0].ID}}
			for _, remove := range []bool{false, true} {
				counts, err := store.walkCheckinCleanup(context.Background(), filter, now, remove)
				if err != nil || counts.Jobs != 0 || counts.Skipped[test.reason] != 1 {
					t.Fatalf("protected task was eligible: %+v %v", counts, err)
				}
			}
			if _, _, err := store.Job(context.Background(), dump.Jobs[0].ID); err != nil {
				t.Fatal("protected task disappeared", err)
			}
		})
	}
}

func TestCheckinCleanupRechecksPreviewAndRollsBackWholeJob(t *testing.T) {
	store, dump := completedCleanupFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	filter := model.CheckinCleanupFilter{JobIDs: []string{dump.Jobs[0].ID}}
	if preview, err := store.PreviewCleanup(ctx, filter, now); err != nil || preview.Jobs != 1 {
		t.Fatal(preview, err)
	}
	if err := store.DB.Model(&model.CheckinRun{}).Where("id = ?", dump.Runs[0].ID).Update("status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if counts, err := store.CleanupJobs(ctx, filter, now); err != nil || counts.Skipped["unconfirmed"] != 1 {
		t.Fatal("cleanup trusted stale preview", counts, err)
	}
	if err := store.DB.Model(&model.CheckinRun{}).Where("id = ?", dump.Runs[0].ID).Update("status", "success").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Callback().Delete().Before("gorm:delete").Register("test:fail-job-delete", func(tx *gorm.DB) {
		if tx.Statement.Table == "checkin_jobs" {
			_ = tx.AddError(errors.New("simulated write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	counts, err := store.CleanupJobs(ctx, filter, now)
	if err == nil || counts.Jobs != 0 {
		t.Fatal("failed transaction counted as deleted", counts, err)
	}
	_, items, err := store.Job(ctx, dump.Jobs[0].ID)
	attempts, _ := store.Attempts(ctx, dump.Runs[0].ID)
	run, _ := store.Run(ctx, dump.Runs[0].ID)
	if err != nil || len(items) != 1 || len(attempts) != 1 || run.DetailsCleanedAt != nil {
		t.Fatal("partial job deletion escaped rollback", err)
	}
}

func TestCheckinCleanupRetentionPagesAndCutoff(t *testing.T) {
	store := NewCheckinStore(checkinTestDB(t))
	ctx, now := context.Background(), time.Now().UTC()
	old, cutoff := now.AddDate(0, 0, -120), now.AddDate(0, 0, -90)
	for i := 0; i < 123; i++ {
		job := model.CheckinJob{ID: uuid.NewString(), IdempotencyKey: uuid.NewString(), Status: "completed", CreatedAt: old, UpdatedAt: old}
		if i == 120 {
			job.FinishedAt = &now
		} // Recently finished old task.
		if i == 121 {
			job.FinishedAt = &cutoff
		} // Boundary is exclusive.
		if i == 122 {
			job.UpdatedAt = now
		} // Legacy task without FinishedAt.
		if err := store.DB.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}
	filter := model.CheckinCleanupFilter{Before: &cutoff}
	preview, err := store.PreviewCleanup(ctx, filter, now)
	if err != nil || preview.Jobs != 120 {
		t.Fatal("retention preview failed", preview, err)
	}
	counts, err := store.CleanupJobs(ctx, filter, now)
	if err != nil || counts.Jobs != 120 {
		t.Fatal("cleanup skipped a page after deletion", counts, err)
	}
	var remaining int64
	store.DB.Model(&model.CheckinJob{}).Count(&remaining)
	if remaining != 3 {
		t.Fatal("retention removed recent or boundary jobs", remaining)
	}
	for _, f := range []model.CheckinCleanupFilter{{}, {JobIDs: []string{"bad"}}, {Before: &cutoff, JobIDs: []string{uuid.NewString()}}, {Before: new(time.Time)}, {Before: func() *time.Time { v := now.Add(time.Hour); return &v }()}} {
		if _, err := store.CleanupJobs(ctx, f, now); err == nil {
			t.Fatal("invalid cleanup selector accepted", f)
		}
	}
}

func TestCheckinJobPaginationSurvivesCursorDeletion(t *testing.T) {
	store := NewCheckinStore(checkinTestDB(t))
	ctx, now := context.Background(), time.Now().UTC()
	for i := 1; i <= 5; i++ {
		job := model.CheckinJob{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), IdempotencyKey: uuid.NewString(), Status: "completed", Source: "manual", CreatedAt: now}
		if err := store.DB.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}
	f := CheckinJobFilter{Limit: 2, Status: "completed", Source: "manual", From: now.Format(time.DateOnly), Until: now.Format(time.DateOnly)}
	first, err := store.JobPage(ctx, f)
	if err != nil || len(first.Items) != 2 || first.NextBefore == "" {
		t.Fatal(first, err)
	}
	if _, err := store.CleanupJobs(ctx, model.CheckinCleanupFilter{JobIDs: []string{first.Items[1].ID}}, now); err != nil {
		t.Fatal(err)
	}
	f.Before = first.NextBefore
	second, err := store.JobPage(ctx, f)
	if err != nil || len(second.Items) != 2 || second.Items[0].ID != "00000000-0000-4000-8000-000000000003" {
		t.Fatal("cursor became invalid after cleanup", second, err)
	}
	f.Before = second.NextBefore
	last, err := store.JobPage(ctx, f)
	if err != nil || len(last.Items) != 1 || last.NextBefore != "" {
		t.Fatal(last, err)
	}
	for _, filter := range []CheckinJobFilter{{Limit: 0}, {Limit: 101}, {Limit: 10, Before: "bad"}, {Limit: 10, Source: "bad"}, {Limit: 10, Status: "bad"}, {Limit: 10, From: "bad"}, {Limit: 10, From: "2026-02-02", Until: "2026-02-01"}} {
		if _, err := store.JobPage(ctx, filter); err == nil {
			t.Fatal("invalid job filter accepted", filter)
		}
	}
	f.Before, f.Source = "", "scheduled"
	if page, err := store.JobPage(ctx, f); err != nil || len(page.Items) != 0 {
		t.Fatal("source filter ignored", page, err)
	}
}

func TestCheckinCleanupBackupSettingsCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"concurrency":4,"site_interval_seconds":2,"http_timeout_seconds":30,"confirm_attempts":3,"confirm_interval_seconds":2,"confirm_timeout_minutes":60}`,
		`{"auto_cleanup":true,"retention_days":30}`,
	} {
		rows, err := normalizeCheckinBackupSettings([]model.Setting{{Key: CheckinSettingKey, Value: raw}, {Key: CheckinCleanupSettingKey, Value: `{"status":"running"}`}})
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		var settings model.CheckinSettings
		if err := json.Unmarshal([]byte(rows[0].Value), &settings); err != nil || settings.AutoCleanup || settings.RetentionDays < 1 {
			t.Fatal("unsafe imported cleanup settings", settings, err)
		}
	}
	store, dump := completedCleanupFixture(t)
	settings := model.DefaultCheckinSettings()
	settings.AutoCleanup, settings.RetentionDays = true, 180
	if err := store.SaveSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	restoreCheckinFixture(t, store.DB, dump)
	restored, err := store.Settings(context.Background())
	if err != nil || restored.AutoCleanup || restored.RetentionDays != 180 {
		t.Fatal("import without settings left automatic cleanup enabled", restored, err)
	}
}

func TestCheckinJobDatesNormalizeStoredOffsets(t *testing.T) {
	store := NewCheckinStore(checkinTestDB(t))
	ctx := context.Background()
	start := time.Date(2026, 11, 15, 23, 59, 0, 0, time.UTC)
	for i, zone := range []*time.Location{time.UTC, time.FixedZone("Asia", 8*3600), time.FixedZone("West", -5*3600)} {
		at := start.Add(time.Duration(i) * time.Minute).In(zone)
		job := model.CheckinJob{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), IdempotencyKey: uuid.NewString(), Status: "completed", CreatedAt: at, UpdatedAt: at}
		if err := store.DB.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.JobPage(ctx, CheckinJobFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "00000000-0000-4000-8000-000000000003" {
		t.Fatal("jobs sorted by wall clock rather than instant", page, err)
	}
	page, err = store.JobPage(ctx, CheckinJobFilter{Limit: 1, Before: page.NextBefore})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "00000000-0000-4000-8000-000000000002" {
		t.Fatal("cursor lost the stored time offset", page, err)
	}
	page, err = store.JobPage(ctx, CheckinJobFilter{Limit: 10, From: "2026-11-15", Until: "2026-11-15"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "00000000-0000-4000-8000-000000000001" {
		t.Fatal("UTC date filter interpreted as local time", page, err)
	}
	cutoff := start.Add(90 * time.Second)
	counts, err := store.CleanupJobs(ctx, model.CheckinCleanupFilter{Before: &cutoff}, start.Add(24*time.Hour))
	if err != nil || counts.Jobs != 2 {
		t.Fatal("retention ignored timestamp offsets", counts, err)
	}
}
