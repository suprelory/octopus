package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const legacyGroupModeRandom model.GroupMode = 2

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 22,
		Up:      removeGroupRandomMode,
	})
}

// removeGroupRandomMode converts persisted groups and presets that used the
// removed random strategy to the stable default, round robin.
func removeGroupRandomMode(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if db.Migrator().HasTable(&model.Group{}) {
		if err := db.Model(&model.Group{}).
			Where("mode = ?", legacyGroupModeRandom).
			Update("mode", model.GroupModeRoundRobin).Error; err != nil {
			return fmt.Errorf("migrate group modes: %w", err)
		}
	}
	if db.Migrator().HasTable(&model.GroupPreset{}) {
		if err := db.Model(&model.GroupPreset{}).
			Where("mode = ?", legacyGroupModeRandom).
			Update("mode", model.GroupModeRoundRobin).Error; err != nil {
			return fmt.Errorf("migrate group preset modes: %w", err)
		}
	}
	return nil
}
