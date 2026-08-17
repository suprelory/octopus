package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026081701,
		Up:      migrateSiteCheckinSchedule,
	})
}

func migrateSiteCheckinSchedule(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SiteAccount{}).
			Where("last_checkin_success_at IS NULL AND last_checkin_at IS NOT NULL AND last_checkin_status = ?", model.SiteExecutionStatusSuccess).
			UpdateColumn("last_checkin_success_at", gorm.Expr("last_checkin_at")).Error; err != nil {
			return err
		}
		// Old random schedules used a rolling 24-hour interval. Rebuild them with
		// the site-local calendar window on the first scan after upgrading.
		return tx.Model(&model.SiteAccount{}).
			Where("auto_checkin = ?", true).
			UpdateColumn("next_auto_checkin_at", nil).Error
	})
}
