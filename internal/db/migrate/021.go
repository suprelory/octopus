package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const (
	legacyGroupHealthAttemptTable  = "group_health_attempts"
	legacyGroupHealthSnapshotTable = "group_health_snapshots"
	legacyGroupHealthSettingKey    = "group_health_enabled"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 21,
		Up:      removeGroupHealthChecks,
	})
}

func removeGroupHealthChecks(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if db.Migrator().HasTable(&model.Setting{}) {
		if err := db.Where("key = ?", legacyGroupHealthSettingKey).Delete(&model.Setting{}).Error; err != nil {
			return fmt.Errorf("delete group health setting: %w", err)
		}
	}

	for _, table := range []string{legacyGroupHealthAttemptTable, legacyGroupHealthSnapshotTable} {
		if db.Migrator().HasTable(table) {
			if err := db.Migrator().DropTable(table); err != nil {
				return fmt.Errorf("drop group health table %s: %w", table, err)
			}
		}
	}
	return nil
}
