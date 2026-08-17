package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSiteCheckinScheduleBackfillsSuccessAndClearsLegacySchedule(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&model.SiteAccount{}); err != nil {
		t.Fatalf("AutoMigrate SiteAccount: %v", err)
	}

	lastCheckin := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	legacyNext := lastCheckin.Add(24 * time.Hour)
	account := model.SiteAccount{
		SiteID:               1,
		Name:                 "legacy",
		CredentialType:       model.SiteCredentialTypeAccessToken,
		Enabled:              true,
		AutoCheckin:          true,
		LastCheckinAt:        &lastCheckin,
		LastCheckinStatus:    model.SiteExecutionStatusSuccess,
		NextAutoCheckinAt:    &legacyNext,
		CheckinIntervalHours: 24,
	}
	if err := database.Create(&account).Error; err != nil {
		t.Fatalf("create legacy account: %v", err)
	}

	if err := migrateSiteCheckinSchedule(database); err != nil {
		t.Fatalf("migrateSiteCheckinSchedule failed: %v", err)
	}
	var reloaded model.SiteAccount
	if err := database.First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.LastCheckinSuccessAt == nil || !reloaded.LastCheckinSuccessAt.Equal(lastCheckin) {
		t.Fatalf("expected last success %s, got %v", lastCheckin, reloaded.LastCheckinSuccessAt)
	}
	if reloaded.NextAutoCheckinAt != nil {
		t.Fatalf("expected legacy next checkin to be cleared, got %s", reloaded.NextAutoCheckinAt)
	}
}
