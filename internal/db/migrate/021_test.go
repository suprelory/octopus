package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRemoveGroupHealthChecks(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("auto migrate settings: %v", err)
	}
	if err := database.Exec(`CREATE TABLE group_health_snapshots (id integer PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create legacy snapshot table: %v", err)
	}
	if err := database.Exec(`CREATE TABLE group_health_attempts (id integer PRIMARY KEY, snapshot_id integer REFERENCES group_health_snapshots(id))`).Error; err != nil {
		t.Fatalf("create legacy attempt table: %v", err)
	}

	settings := []model.Setting{
		{Key: model.SettingKey(legacyGroupHealthSettingKey), Value: "true"},
		{Key: model.SettingKeyProxyURL, Value: "https://proxy.example"},
	}
	if err := database.Create(&settings).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}

	if err := removeGroupHealthChecks(database); err != nil {
		t.Fatalf("removeGroupHealthChecks failed: %v", err)
	}
	if err := removeGroupHealthChecks(database); err != nil {
		t.Fatalf("removeGroupHealthChecks should be idempotent: %v", err)
	}
	if database.Migrator().HasTable(legacyGroupHealthAttemptTable) {
		t.Fatal("expected legacy group health attempt table to be removed")
	}
	if database.Migrator().HasTable(legacyGroupHealthSnapshotTable) {
		t.Fatal("expected legacy group health snapshot table to be removed")
	}

	var groupHealthSettings int64
	if err := database.Model(&model.Setting{}).Where("key = ?", legacyGroupHealthSettingKey).Count(&groupHealthSettings).Error; err != nil {
		t.Fatalf("count legacy group health settings: %v", err)
	}
	if groupHealthSettings != 0 {
		t.Fatalf("expected legacy group health setting to be removed, got %d", groupHealthSettings)
	}
	var proxySetting model.Setting
	if err := database.First(&proxySetting, "key = ?", model.SettingKeyProxyURL).Error; err != nil {
		t.Fatalf("reload unrelated setting: %v", err)
	}
}

func TestRemoveGroupHealthChecksRejectsNilDB(t *testing.T) {
	if err := removeGroupHealthChecks(nil); err == nil {
		t.Fatal("expected nil database error")
	}
}
