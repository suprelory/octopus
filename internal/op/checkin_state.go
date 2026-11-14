package op

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Check-in SQL may contain proxy credentials or encrypted account material.
// Keep it out of the application's optional SQL debug logger.
func NewCheckinStore(conn *gorm.DB) *CheckinStore {
	return &CheckinStore{DB: conn.Session(&gorm.Session{Logger: logger.Discard})}
}

func (s *CheckinStore) RecordSupport(ctx context.Context, site model.CheckinSite, support string, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Model(&model.CheckinSite{}).Where("id = ? AND revision = ?", site.ID, site.Revision).
		Updates(map[string]any{"support": support, "support_revision": site.Revision, "verified_at": now}).Error
}

func (s *CheckinStore) ExpireConfirmations(ctx context.Context, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := tx.Model(&model.CheckinRun{}).Select("id").Where("status = ? AND confirm_deadline <= ?", "pending", now)
		if err := tx.Model(&model.CheckinAccount{}).Where("last_run_id IN (?)", ids).Update("last_status", "manual_required").Error; err != nil {
			return err
		}
		return tx.Model(&model.CheckinRun{}).Where("status = ? AND confirm_deadline <= ?", "pending", now).
			Updates(map[string]any{"status": "manual_required", "reason": "confirmation_expired", "phase": "finished", "next_confirm_at": nil}).Error
	})
}

func (s *CheckinStore) Schedule(ctx context.Context, account model.CheckinAccount, next time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Model(&model.CheckinAccount{}).
		Where("id = ? AND revision = ? AND last_run_id = ? AND deleted_at IS NULL", account.ID, account.Revision, account.LastRunID).
		Update("next_run_at", next).Error
}
