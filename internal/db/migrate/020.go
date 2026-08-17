package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const legacySiteChannelOutlierTable = "site_channel_outlier_states"

var legacyOutlierSettingKeys = []string{
	"outlier_retire_enabled",
	"outlier_retire_interval",
	"outlier_window_capacity",
	"outlier_window_minutes",
	"outlier_min_samples",
	"outlier_fail_rate_pct",
	"outlier_consec_fails",
	"outlier_recover_streak",
	"outlier_reap_minutes",
	"outlier_cf_recover_minutes",
}

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 20,
		Up:      removePassiveOutlierRetirement,
	})
}

func removePassiveOutlierRetirement(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if db.Migrator().HasTable(legacySiteChannelOutlierTable) {
		var retiredChannelIDs []int
		if err := db.Table(legacySiteChannelOutlierTable).
			Where("status = ?", "retired").
			Pluck("channel_id", &retiredChannelIDs).Error; err != nil {
			return fmt.Errorf("list passively retired channels: %w", err)
		}
		if len(retiredChannelIDs) > 0 && db.Migrator().HasTable(&model.Channel{}) {
			if err := db.Model(&model.Channel{}).
				Where("id IN ?", retiredChannelIDs).
				Update("enabled", true).Error; err != nil {
				return fmt.Errorf("restore passively retired channels: %w", err)
			}
		}
	}

	if db.Migrator().HasTable(&model.Setting{}) {
		if err := db.Where("key IN ?", legacyOutlierSettingKeys).Delete(&model.Setting{}).Error; err != nil {
			return fmt.Errorf("delete passive outlier settings: %w", err)
		}
	}

	if db.Migrator().HasTable(legacySiteChannelOutlierTable) {
		if err := db.Migrator().DropTable(legacySiteChannelOutlierTable); err != nil {
			return fmt.Errorf("drop passive outlier state table: %w", err)
		}
	}
	return nil
}
