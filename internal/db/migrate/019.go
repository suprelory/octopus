package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 19,
		Up:      migrateGroupEmptyResponseDetection,
	})
}

// migrateGroupEmptyResponseDetection 为存量分组/预设回填空回检测开关。
//
// 该字段刻意不带 gorm:"default:true"：GORM 在 Create 时会跳过声明了 default 的
// 零值字段，让数据库默认值生效——那样用户在 UI 上关掉空回检测后，新建分组会被
// 静默改回 true。去掉 default 标签后 false 能正常落库，代价是 AutoMigrate 加列时
// 存量行为 NULL，因此在这里显式回填为 true，保持升级前后的行为一致。
func migrateGroupEmptyResponseDetection(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if db.Migrator().HasTable(&model.Group{}) {
		if !db.Migrator().HasColumn(&model.Group{}, "EmptyResponseDetection") {
			if err := db.Migrator().AddColumn(&model.Group{}, "EmptyResponseDetection"); err != nil {
				return err
			}
		}
		if err := db.Model(&model.Group{}).
			Where("empty_response_detection IS NULL").
			Update("empty_response_detection", true).Error; err != nil {
			return err
		}
	}

	if db.Migrator().HasTable(&model.GroupPreset{}) {
		if !db.Migrator().HasColumn(&model.GroupPreset{}, "EmptyResponseDetection") {
			if err := db.Migrator().AddColumn(&model.GroupPreset{}, "EmptyResponseDetection"); err != nil {
				return err
			}
		}
		if err := db.Model(&model.GroupPreset{}).
			Where("empty_response_detection IS NULL").
			Update("empty_response_detection", true).Error; err != nil {
			return err
		}
	}

	return nil
}
