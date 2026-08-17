package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRemovePassiveOutlierRetirement(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&model.Channel{}, &model.Setting{}); err != nil {
		t.Fatalf("auto migrate current models: %v", err)
	}
	if err := database.Exec(`CREATE TABLE site_channel_outlier_states (channel_id integer PRIMARY KEY, status varchar(16) NOT NULL)`).Error; err != nil {
		t.Fatalf("create legacy outlier table: %v", err)
	}

	retired := model.Channel{Name: "retired", Enabled: false}
	active := model.Channel{Name: "active", Enabled: false}
	if err := database.Create(&retired).Error; err != nil {
		t.Fatalf("create retired channel: %v", err)
	}
	if err := database.Create(&active).Error; err != nil {
		t.Fatalf("create active channel: %v", err)
	}
	if err := database.Model(&model.Channel{}).Where("id IN ?", []int{retired.ID, active.ID}).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable test channels: %v", err)
	}
	if err := database.Exec(
		`INSERT INTO site_channel_outlier_states (channel_id, status) VALUES (?, ?), (?, ?)`,
		retired.ID, "retired", active.ID, "active",
	).Error; err != nil {
		t.Fatalf("insert legacy outlier states: %v", err)
	}
	settings := []model.Setting{
		{Key: model.SettingKey("outlier_retire_enabled"), Value: "true"},
		{Key: model.SettingKey("outlier_window_minutes"), Value: "10"},
		{Key: model.SettingKeyProxyURL, Value: "https://proxy.example"},
	}
	if err := database.Create(&settings).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}

	if err := removePassiveOutlierRetirement(database); err != nil {
		t.Fatalf("removePassiveOutlierRetirement failed: %v", err)
	}
	if err := removePassiveOutlierRetirement(database); err != nil {
		t.Fatalf("removePassiveOutlierRetirement should be idempotent: %v", err)
	}
	if database.Migrator().HasTable(legacySiteChannelOutlierTable) {
		t.Fatal("expected legacy outlier table to be removed")
	}

	var reloadedRetired model.Channel
	if err := database.First(&reloadedRetired, retired.ID).Error; err != nil {
		t.Fatalf("reload retired channel: %v", err)
	}
	if !reloadedRetired.Enabled {
		t.Fatal("expected passively retired channel to be restored")
	}
	var reloadedActive model.Channel
	if err := database.First(&reloadedActive, active.ID).Error; err != nil {
		t.Fatalf("reload active channel: %v", err)
	}
	if reloadedActive.Enabled {
		t.Fatal("expected channel without retired state to remain disabled")
	}

	var outlierSettings int64
	if err := database.Model(&model.Setting{}).Where("key IN ?", legacyOutlierSettingKeys).Count(&outlierSettings).Error; err != nil {
		t.Fatalf("count legacy settings: %v", err)
	}
	if outlierSettings != 0 {
		t.Fatalf("expected legacy outlier settings to be removed, got %d", outlierSettings)
	}
	var proxySetting model.Setting
	if err := database.First(&proxySetting, "key = ?", model.SettingKeyProxyURL).Error; err != nil {
		t.Fatalf("reload unrelated setting: %v", err)
	}
}
