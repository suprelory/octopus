package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRemoveGroupRandomMode(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&model.Group{}, &model.GroupPreset{}); err != nil {
		t.Fatalf("auto migrate groups: %v", err)
	}
	groups := []model.Group{
		{Name: "legacy-random", Mode: legacyGroupModeRandom},
		{Name: "round-robin", Mode: model.GroupModeRoundRobin},
	}
	if err := database.Create(&groups).Error; err != nil {
		t.Fatalf("create groups: %v", err)
	}
	presets := []model.GroupPreset{
		{GroupID: groups[0].ID, Name: "legacy", Mode: legacyGroupModeRandom},
		{GroupID: groups[1].ID, Name: "current", Mode: model.GroupModeWeighted},
	}
	if err := database.Create(&presets).Error; err != nil {
		t.Fatalf("create presets: %v", err)
	}

	if err := removeGroupRandomMode(database); err != nil {
		t.Fatalf("removeGroupRandomMode failed: %v", err)
	}
	if err := removeGroupRandomMode(database); err != nil {
		t.Fatalf("removeGroupRandomMode should be idempotent: %v", err)
	}

	var migratedGroup model.Group
	if err := database.First(&migratedGroup, groups[0].ID).Error; err != nil {
		t.Fatalf("reload migrated group: %v", err)
	}
	if migratedGroup.Mode != model.GroupModeRoundRobin {
		t.Fatalf("expected legacy group to become round robin, got %d", migratedGroup.Mode)
	}
	var migratedPreset model.GroupPreset
	if err := database.First(&migratedPreset, presets[0].ID).Error; err != nil {
		t.Fatalf("reload migrated preset: %v", err)
	}
	if migratedPreset.Mode != model.GroupModeRoundRobin {
		t.Fatalf("expected legacy preset to become round robin, got %d", migratedPreset.Mode)
	}
	var currentGroup model.Group
	if err := database.First(&currentGroup, groups[1].ID).Error; err != nil {
		t.Fatalf("reload current group: %v", err)
	}
	if currentGroup.Mode != model.GroupModeRoundRobin {
		t.Fatalf("expected current group to remain unchanged, got %d", currentGroup.Mode)
	}
}

func TestRemoveGroupRandomModeRejectsNilDB(t *testing.T) {
	if err := removeGroupRandomMode(nil); err == nil {
		t.Fatal("expected nil database error")
	}
}
