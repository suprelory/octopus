package migrate

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const (
	legacyCompatibilityCleanupVersion = 2026090501
	legacySiteCheckinIntervalSetting  = "site_checkin_interval"
)

type legacyGroupCompatibilityColumns struct {
	ID                     int
	EmptyResponseDetection bool `gorm:"column:empty_response_detection"`
}

func (legacyGroupCompatibilityColumns) TableName() string { return "groups" }

type legacyGroupPresetCompatibilityColumns struct {
	ID                     int
	EmptyResponseDetection bool `gorm:"column:empty_response_detection"`
}

func (legacyGroupPresetCompatibilityColumns) TableName() string { return "group_presets" }

type legacyChannelCompatibilityColumns struct {
	ID           int
	Proxy        bool
	ChannelProxy *string `gorm:"column:channel_proxy"`
}

func (legacyChannelCompatibilityColumns) TableName() string { return "channels" }

type legacySiteCompatibilityColumns struct {
	ID             int
	Proxy          bool
	SiteProxy      *string `gorm:"column:site_proxy"`
	UseSystemProxy bool    `gorm:"column:use_system_proxy"`
}

func (legacySiteCompatibilityColumns) TableName() string { return "sites" }

type legacySiteAccountCompatibilityColumns struct {
	ID           int
	AccountProxy *string `gorm:"column:account_proxy"`
}

func (legacySiteAccountCompatibilityColumns) TableName() string { return "site_accounts" }

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: legacyCompatibilityCleanupVersion,
		Up:      removeLegacyCompatibilityState,
	})
}

func removeLegacyCompatibilityState(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if db.Migrator().HasTable(&model.Setting{}) {
		if err := db.Model(&model.Setting{}).
			Where("key = ? AND LOWER(TRIM(value)) = ?", model.SettingKeyProjectedChannelAutoGroupEnabled, "true").
			UpdateColumn("value", "1").Error; err != nil {
			return fmt.Errorf("normalize enabled projected auto-group setting: %w", err)
		}
		if err := db.Model(&model.Setting{}).
			Where("key = ? AND (LOWER(TRIM(value)) = ? OR TRIM(value) = ?)", model.SettingKeyProjectedChannelAutoGroupEnabled, "false", "").
			UpdateColumn("value", "0").Error; err != nil {
			return fmt.Errorf("normalize disabled projected auto-group setting: %w", err)
		}
		if err := db.Where("key = ?", legacySiteCheckinIntervalSetting).Delete(&model.Setting{}).Error; err != nil {
			return fmt.Errorf("remove legacy site check-in setting: %w", err)
		}
	}

	legacyColumns := []struct {
		table  string
		column string
	}{
		{table: "groups", column: "empty_response_detection"},
		{table: "group_presets", column: "empty_response_detection"},
		{table: "channels", column: "proxy"},
		{table: "channels", column: "channel_proxy"},
		{table: "sites", column: "proxy"},
		{table: "sites", column: "site_proxy"},
		{table: "sites", column: "use_system_proxy"},
		{table: "site_accounts", column: "account_proxy"},
	}
	for _, legacyColumn := range legacyColumns {
		if err := dropColumnIfPresent(db, legacyColumn.table, legacyColumn.column); err != nil {
			return err
		}
	}
	return nil
}

func dropColumnIfPresent(db *gorm.DB, table, column string) error {
	if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, column) {
		return nil
	}
	if db.Dialector != nil && db.Dialector.Name() == "sqlite" {
		// glebarez/sqlite implements Migrator.DropColumn by rebuilding the
		// table. With foreign_keys enabled, dropping a referenced parent table
		// during that rebuild fails even though SQLite supports native DROP
		// COLUMN without removing the parent table.
		var quotedTable, quotedColumn strings.Builder
		db.Dialector.QuoteTo(&quotedTable, table)
		db.Dialector.QuoteTo(&quotedColumn, column)
		if err := db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", quotedTable.String(), quotedColumn.String())).Error; err != nil {
			return fmt.Errorf("drop legacy column %s.%s: %w", table, column, err)
		}
		return nil
	}
	// Other dialects can use GORM's migrator; the migration-only schema keeps
	// the removed field available for dialects that rebuild tables internally.
	var migrationModel any
	switch table {
	case "groups":
		migrationModel = &legacyGroupCompatibilityColumns{}
	case "group_presets":
		migrationModel = &legacyGroupPresetCompatibilityColumns{}
	case "channels":
		migrationModel = &legacyChannelCompatibilityColumns{}
	case "sites":
		migrationModel = &legacySiteCompatibilityColumns{}
	case "site_accounts":
		migrationModel = &legacySiteAccountCompatibilityColumns{}
	default:
		return fmt.Errorf("unsupported legacy table %s", table)
	}
	if err := db.Migrator().DropColumn(migrationModel, column); err != nil {
		return fmt.Errorf("drop legacy column %s.%s: %w", table, column, err)
	}
	return nil
}
