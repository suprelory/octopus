package op

import (
	"context"
	"errors"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A lease outlives a bounded worker round. A recovered item always reuses its
// daily run, including the durable submit marker.
const CheckinLeaseDuration = 3 * time.Minute

func (s *CheckinStore) Recover(ctx context.Context, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.CheckinJobItem{}).Where("state = ? AND lease_until <= ?", "running", now).Updates(map[string]any{"state": "queued", "lease_owner": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		var canceled []string
		if err := tx.Model(&model.CheckinJobItem{}).Joins("JOIN checkin_jobs ON checkin_jobs.id = checkin_job_items.job_id").Where("checkin_job_items.state = ? AND checkin_jobs.status = ?", "queued", "canceled").Distinct().Pluck("checkin_job_items.job_id", &canceled).Error; err != nil {
			return err
		}
		for _, id := range canceled {
			if err := tx.Model(&model.CheckinJobItem{}).Where("job_id = ? AND state = ?", id, "queued").Updates(map[string]any{"state": "done", "outcome": "skipped"}).Error; err != nil {
				return err
			}
			if err := reconcileCheckinJob(tx, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *CheckinStore) Queue(ctx context.Context) ([]model.CheckinJobItem, error) {
	items := []model.CheckinJobItem{}
	err := s.DB.WithContext(ctx).Model(&model.CheckinJobItem{}).
		Joins("JOIN checkin_jobs ON checkin_jobs.id = checkin_job_items.job_id").
		Where("checkin_job_items.state = ? AND checkin_jobs.status IN ?", "queued", []string{"queued", "running"}).
		Order("checkin_job_items.created_at, checkin_job_items.id").Limit(100).Find(&items).Error
	return items, err
}

func (s *CheckinStore) Claim(ctx context.Context, item *model.CheckinJobItem, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	owner := uuid.NewString()
	until := now.Add(CheckinLeaseDuration)
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.CheckinJob
		if err := tx.First(&job, "id = ?", item.JobID).Error; err != nil {
			return err
		}
		if job.Status != "queued" && job.Status != "running" {
			return ErrCheckinConflict
		}
		result := tx.Model(&model.CheckinAccount{}).Where("id = ? AND (lease_until IS NULL OR lease_until <= ?)", item.AccountID, now).Updates(map[string]any{"lease_owner": owner, "lease_until": until})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		result = tx.Model(&model.CheckinJobItem{}).Where("id = ? AND state = ?", item.ID, "queued").Updates(map[string]any{"state": "running", "lease_owner": owner, "lease_until": until})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		return tx.Model(&model.CheckinJob{}).Where("id = ? AND status = ?", item.JobID, "queued").Update("status", "running").Error
	})
	if errors.Is(err, ErrCheckinConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	item.State, item.LeaseOwner, item.LeaseUntil = "running", owner, &until
	return true, nil
}

func (s *CheckinStore) BeginRun(ctx context.Context, item model.CheckinJobItem, site model.CheckinSite, account model.CheckinAccount, fingerprint, source string, now time.Time) (model.CheckinRun, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	run := model.CheckinRun{ID: uuid.NewString(), SiteID: site.ID, AccountID: account.ID, CheckinDate: item.CheckinDate, SiteName: site.Config.Name, AccountName: account.Name, Timezone: site.Config.Timezone, Fingerprint: fingerprint, Phase: "precheck", Status: "running", Source: source, RewardState: "unknown", CreatedAt: now, UpdatedAt: now}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if item.ConfirmOnly {
			run = model.CheckinRun{}
			if err := tx.First(&run, "id = ? AND account_id = ?", item.RunID, account.ID).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&run).Error; err != nil {
				return err
			}
			// Clear the proposed ID: GORM otherwise adds it to the lookup after a
			// daily uniqueness conflict, hiding the already-existing ledger.
			run = model.CheckinRun{}
			if err := tx.Where("account_id = ? AND checkin_date = ?", account.ID, item.CheckinDate).First(&run).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.CheckinJobItem{}).Where("id = ? AND lease_owner = ?", item.ID, item.LeaseOwner).Update("run_id", run.ID).Error
	})
	return run, err
}

func (s *CheckinStore) SaveRunProxy(ctx context.Context, item model.CheckinJobItem, run model.CheckinRun, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account model.CheckinAccount
		if err := tx.First(&account, "id = ? AND lease_owner = ? AND lease_until > ? AND deleted_at IS NULL AND enabled = ?", item.AccountID, item.LeaseOwner, now, true).Error; err != nil {
			return err
		}
		result := tx.Model(&model.CheckinRun{}).Where("id = ? AND account_id = ?", run.ID, item.AccountID).Updates(map[string]any{"fingerprint": run.Fingerprint, "proxy_fingerprint": run.ProxyFingerprint, "before_unchecked": run.BeforeUnchecked})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		return nil
	})
}

func (s *CheckinStore) MarkSubmitted(ctx context.Context, item model.CheckinJobItem, run *model.CheckinRun, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return checkinIntentTransaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
		var account model.CheckinAccount
		if err := tx.First(&account, "id = ? AND lease_owner = ? AND lease_until > ? AND deleted_at IS NULL AND enabled = ?", item.AccountID, item.LeaseOwner, now, true).Error; err != nil {
			return err
		}
		// This write and its check must succeed before any side-effecting request.
		result := tx.Model(&model.CheckinRun{}).Where("id = ? AND (submit_intent_at IS NULL OR safe_to_retry = ?)", run.ID, true).
			Updates(map[string]any{"submit_intent_at": now, "safe_to_retry": false, "before_unchecked": run.BeforeUnchecked, "phase": "submit_intent", "status": "running", "fingerprint": run.Fingerprint, "proxy_fingerprint": run.ProxyFingerprint, "confirm_deadline": run.ConfirmDeadline})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		run.SubmitIntentAt, run.SafeToRetry, run.Phase = &now, false, "submit_intent"
		return nil
	})
}

func (s *CheckinStore) AddAttempt(ctx context.Context, attempt model.CheckinAttempt) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if attempt.ID == "" {
		attempt.ID = uuid.NewString()
	}
	return s.DB.WithContext(ctx).Create(&attempt).Error
}

func (s *CheckinStore) Complete(ctx context.Context, item model.CheckinJobItem, run *model.CheckinRun, outcome string, nextRun *time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.CheckinJobItem
		if err := tx.First(&current, "id = ? AND state = ? AND lease_owner = ?", item.ID, "running", item.LeaseOwner).Error; err != nil {
			return err
		}
		var account model.CheckinAccount
		if err := tx.First(&account, "id = ? AND lease_owner = ?", item.AccountID, item.LeaseOwner).Error; err != nil {
			return err
		}
		if run != nil {
			var previous model.CheckinRun
			if err := tx.First(&previous, "id = ?", run.ID).Error; err != nil {
				return err
			}
			if previous.Status != "success" && previous.Status != "already_checked_in" {
				if err := tx.Model(&model.CheckinRun{}).Where("id = ?", run.ID).Select("Phase", "Status", "Reason", "Fingerprint", "ProxyFingerprint", "SubmitIntentAt", "SafeToRetry", "BeforeUnchecked", "NextConfirmAt", "ConfirmDeadline", "RewardMicros", "RewardUnit", "RewardState", "UpdatedAt").Updates(run).Error; err != nil {
					return err
				}
				account.LastRunID, account.LastStatus = run.ID, run.Status
				if run.Status == "success" || run.Status == "already_checked_in" {
					if account.LastSuccessDate < run.CheckinDate {
						account.LastSuccessDate = run.CheckinDate
					}
					account.FailureCount = 0
				} else if run.Status == "failed" && previous.Status != "failed" {
					account.FailureCount++
				}
				account.NextRunAt = nextRun
				if err := tx.Model(&model.CheckinAccount{}).Where("id = ?", account.ID).Select("LastRunID", "LastStatus", "LastSuccessDate", "FailureCount", "NextRunAt", "UpdatedAt").Updates(&account).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&model.CheckinJobItem{}).Where("id = ? AND lease_owner = ?", item.ID, item.LeaseOwner).Updates(map[string]any{"state": "done", "outcome": outcome, "lease_owner": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CheckinAccount{}).Where("id = ? AND lease_owner = ?", item.AccountID, item.LeaseOwner).Updates(map[string]any{"lease_owner": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		return reconcileCheckinJob(tx, item.JobID)
	})
}

func reconcileCheckinJob(tx *gorm.DB, jobID string) error {
	var job model.CheckinJob
	if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
		return err
	}
	var counts []struct {
		State   string
		Outcome string
		Count   int
	}
	if err := tx.Model(&model.CheckinJobItem{}).Select("state, outcome, count(*) AS count").Where("job_id = ?", jobID).Group("state, outcome").Scan(&counts).Error; err != nil {
		return err
	}
	job.Completed, job.Success, job.Already, job.Failed, job.Pending, job.Skipped = 0, 0, 0, 0, 0, 0
	for _, count := range counts {
		if count.State != "done" {
			continue
		}
		job.Completed += count.Count
		switch count.Outcome {
		case "success":
			job.Success += count.Count
		case "already_checked_in":
			job.Already += count.Count
		case "pending", "manual_required":
			job.Pending += count.Count
		case "failed":
			job.Failed += count.Count
		default:
			job.Skipped += count.Count
		}
	}
	if job.Status != "canceled" && job.Status != "interrupted" && job.Completed >= job.Total {
		job.Status = "completed"
		if job.Failed > 0 {
			job.Status = "completed_with_errors"
		}
	}
	return tx.Model(&model.CheckinJob{}).Where("id = ?", job.ID).Select("Status", "Completed", "Success", "Already", "Failed", "Pending", "Skipped", "UpdatedAt").Updates(&job).Error
}

func (s *CheckinStore) HasQueuedAccount(ctx context.Context, accountID string) (bool, error) {
	var count int64
	err := s.DB.WithContext(ctx).Model(&model.CheckinJobItem{}).Where("account_id = ? AND state IN ?", accountID, []string{"queued", "running"}).Count(&count).Error
	return count > 0, err
}
