package op

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Operational history is deliberately excluded from database backups.
const CheckinCleanupSettingKey model.SettingKey = "checkin_cleanup_status"

func NormalizeCheckinCleanup(f model.CheckinCleanupFilter, now time.Time) (model.CheckinCleanupFilter, error) {
	if (len(f.JobIDs) == 0) == (f.Before == nil) || len(f.JobIDs) > 1000 {
		return f, model.InvalidCheckin("select 1 to 1000 jobs or a cutoff time")
	}
	if f.Before != nil {
		at := f.Before.UTC()
		if at.IsZero() || at.After(now) {
			return f, model.InvalidCheckin("cleanup cutoff must be in the past")
		}
		f.Before = &at
	}
	f.JobIDs = slices.Clone(f.JobIDs)
	for i, id := range f.JobIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return f, model.InvalidCheckin("invalid cleanup job ID")
		}
		f.JobIDs[i] = parsed.String()
	}
	slices.Sort(f.JobIDs)
	f.JobIDs = slices.Compact(f.JobIDs)
	return f, nil
}

func (s *CheckinStore) PreviewCleanup(ctx context.Context, filter model.CheckinCleanupFilter, now time.Time) (model.CheckinCleanupCounts, error) {
	return s.walkCheckinCleanup(ctx, filter, now, false)
}

func (s *CheckinStore) CleanupJobs(ctx context.Context, filter model.CheckinCleanupFilter, now time.Time) (model.CheckinCleanupCounts, error) {
	return s.walkCheckinCleanup(ctx, filter, now, true)
}

func (s *CheckinStore) walkCheckinCleanup(ctx context.Context, filter model.CheckinCleanupFilter, now time.Time, remove bool) (model.CheckinCleanupCounts, error) {
	counts := model.CheckinCleanupCounts{Skipped: map[string]int64{}}
	f, err := NormalizeCheckinCleanup(filter, now)
	if err != nil {
		return counts, err
	}
	process := func(ids []string) error {
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			one, reason, err := s.cleanupJob(ctx, id, f, now, remove)
			if err != nil {
				return err
			}
			counts.Matched++
			if reason != "" {
				counts.Skipped[reason]++
			} else {
				counts.Jobs++
				counts.Items += one.Items
				counts.Attempts += one.Attempts
			}
		}
		return nil
	}
	if len(f.JobIDs) > 0 {
		err := process(f.JobIDs)
		return counts, err
	}
	// A bounded ID page and one transaction per job keep the queue available and
	// avoid skipping rows as preceding pages are deleted.
	last := ""
	created, timeArg := checkinJobTimeSQL(s.DB, "created_at")
	finished, _ := checkinJobTimeSQL(s.DB, "COALESCE(finished_at, updated_at)")
	for {
		var ids []string
		err := s.DB.WithContext(ctx).Model(&model.CheckinJob{}).
			Where("id > ? AND "+created+" <= "+timeArg+" AND "+finished+" < "+timeArg, last, now, *f.Before).
			Order("id").Limit(50).Pluck("id", &ids).Error
		if err != nil || len(ids) == 0 {
			return counts, err
		}
		if err := process(ids); err != nil {
			return counts, err
		}
		last = ids[len(ids)-1]
	}
}

func checkinCleanupRunIDs(tx *gorm.DB, jobID string) *gorm.DB {
	// Include account/date links for jobs skipped before BeginRun, as well as
	// explicit run links. Several jobs may share the same daily ledger.
	return tx.Raw(`SELECT r.id FROM checkin_runs r
		JOIN checkin_job_items i ON i.account_id = r.account_id AND i.checkin_date = r.checkin_date WHERE i.job_id = ?
		UNION SELECT run_id FROM checkin_job_items WHERE job_id = ? AND run_id <> ''
		UNION SELECT run_id FROM checkin_attempts WHERE job_id = ?`, jobID, jobID, jobID)
}

func checkinCleanupEligibility(tx *gorm.DB, job model.CheckinJob, now time.Time) (string, error) {
	if !slices.Contains([]string{"completed", "completed_with_errors", "canceled", "interrupted"}, job.Status) || job.Completed != job.Total {
		return "active", nil
	}
	var count int64
	if err := tx.Model(&model.CheckinJobItem{}).Where("job_id = ? AND state <> ?", job.ID, "done").Count(&count).Error; err != nil || count > 0 {
		return "active", err
	}
	if err := tx.Model(&model.CheckinJobItem{}).Where("job_id = ?", job.ID).Count(&count).Error; err != nil || count != int64(job.Total) {
		return "changed", err
	}
	if err := tx.Model(&model.CheckinRun{}).Where("id IN (?) AND (status IN ? OR next_confirm_at IS NOT NULL)", checkinCleanupRunIDs(tx, job.ID), []string{"running", "pending", "manual_required"}).Count(&count).Error; err != nil || count > 0 {
		return "unconfirmed", err
	}
	accounts := tx.Model(&model.CheckinJobItem{}).Select("account_id").Where("job_id = ?", job.ID)
	if err := tx.Model(&model.CheckinAccount{}).Where("id IN (?) AND lease_until > ?", accounts, now).Count(&count).Error; err != nil || count > 0 {
		return "leased", err
	}
	linkedIDs := tx.Raw(`SELECT id FROM checkin_job_items WHERE job_id = ?
		UNION SELECT id FROM checkin_job_items WHERE run_id IN (?)
		UNION SELECT other.id FROM checkin_job_items other JOIN checkin_job_items linked
		ON linked.account_id = other.account_id AND linked.checkin_date = other.checkin_date WHERE linked.job_id = ?`, job.ID, checkinCleanupRunIDs(tx, job.ID), job.ID)
	linkedItems := tx.Model(&model.CheckinJobItem{}).Where("id IN (?)", linkedIDs)
	if err := linkedItems.Session(&gorm.Session{}).Where("lease_until > ?", now).Count(&count).Error; err != nil || count > 0 {
		return "leased", err
	}
	if err := linkedItems.Where("state <> ?", "done").Count(&count).Error; err != nil || count > 0 {
		return "active", err
	}
	return "", nil
}

func (s *CheckinStore) cleanupJob(ctx context.Context, id string, filter model.CheckinCleanupFilter, now time.Time, remove bool) (model.CheckinCleanupCounts, string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var counts model.CheckinCleanupCounts
	var reason string
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.CheckinJob
		if err := tx.First(&job, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				reason = "missing"
				return nil
			}
			return err
		}
		finished := job.UpdatedAt
		if job.FinishedAt != nil {
			finished = *job.FinishedAt
		}
		if filter.Before != nil && (!finished.Before(*filter.Before) || job.CreatedAt.After(now)) {
			reason = "changed"
			return nil
		}
		var err error
		reason, err = checkinCleanupEligibility(tx, job, now)
		if err != nil || reason != "" {
			return err
		}
		counts.Items = int64(job.Total)
		if err := tx.Model(&model.CheckinAttempt{}).Where("job_id = ?", id).Count(&counts.Attempts).Error; err != nil {
			return err
		}
		if !remove {
			return nil
		}
		// Only the diagnostic marker changes; ledger status, reward and UpdatedAt
		// remain untouched, including any references from accounts/notifications.
		// Materialize IDs before updating: MySQL disallows selecting from the
		// update target inside this subquery. Batches also cap bind parameters.
		var runIDs []string
		if err := tx.Table("(?) AS cleanup_runs", checkinCleanupRunIDs(tx, id)).Pluck("id", &runIDs).Error; err != nil {
			return err
		}
		for batch := range slices.Chunk(runIDs, 200) {
			if err := tx.Model(&model.CheckinRun{}).Where("id IN ?", batch).UpdateColumn("details_cleaned_at", now).Error; err != nil {
				return err
			}
		}
		for _, row := range []any{&model.CheckinAttempt{}, &model.CheckinJobItem{}} {
			if err := tx.Where("job_id = ?", id).Delete(row).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&job).Error
	})
	return counts, reason, err
}

func (s *CheckinStore) CleanupReport(ctx context.Context) (*model.CheckinCleanupReport, error) {
	var row model.Setting
	if err := s.DB.WithContext(ctx).Where(&model.Setting{Key: CheckinCleanupSettingKey}).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var report model.CheckinCleanupReport
	if err := json.Unmarshal([]byte(row.Value), &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (s *CheckinStore) SaveCleanupReport(ctx context.Context, report model.CheckinCleanupReport) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	value, err := json.Marshal(report)
	if err != nil {
		return err
	}
	row := model.Setting{Key: CheckinCleanupSettingKey, Value: string(value)}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&row).Error
}
