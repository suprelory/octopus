package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateChannelPassthroughMode,
	})
}

func migrateChannelPassthroughMode(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Channel{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.Channel{}, "PassthroughMode") {
		if err := db.Migrator().AddColumn(&model.Channel{}, "PassthroughMode"); err != nil {
			return err
		}
	}
	return db.Model(&model.Channel{}).
		Where("passthrough_mode IS NULL OR passthrough_mode = ''").
		Update("passthrough_mode", string(model.ChannelPassthroughModeAuto)).Error
}
