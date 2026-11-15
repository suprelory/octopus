package op

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (s *CheckinStore) ExpireConfirmations(ctx context.Context, now time.Time) ([]model.CheckinRun, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var rows []model.CheckinRun
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status = ? AND confirm_deadline <= ?", "pending", now).Order("confirm_deadline, id").Limit(1000).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, len(rows))
		for i := range rows {
			ids[i] = rows[i].ID
			rows[i].Status, rows[i].Reason, rows[i].Phase, rows[i].NextConfirmAt, rows[i].UpdatedAt = "manual_required", "confirmation_expired", "finished", nil, now
		}
		if err := tx.Model(&model.CheckinAccount{}).Where("last_run_id IN ?", ids).Update("last_status", "manual_required").Error; err != nil {
			return err
		}
		return tx.Model(&model.CheckinRun{}).Where("id IN ?", ids).
			Updates(map[string]any{"status": "manual_required", "reason": "confirmation_expired", "phase": "finished", "next_confirm_at": nil, "updated_at": now}).Error
	})
	return rows, err
}

func (s *CheckinStore) Schedule(ctx context.Context, account model.CheckinAccount, next time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Model(&model.CheckinAccount{}).
		Where("id = ? AND revision = ? AND last_run_id = ? AND deleted_at IS NULL", account.ID, account.Revision, account.LastRunID).
		Update("next_run_at", next).Error
}
