package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateProxyPoolSkipsSchemasWithoutLegacyColumns(t *testing.T) {
	database := openProxyMigrationTestDB(t)
	if err := migrateProxyPool(database); err != nil {
		t.Fatalf("migrate current proxy schema: %v", err)
	}
}

func TestMigrateProxyPoolReadsLegacyColumnsWhenPresent(t *testing.T) {
	database := openProxyMigrationTestDB(t)
	for _, statement := range []string{
		"ALTER TABLE channels ADD COLUMN proxy numeric NOT NULL DEFAULT 0",
		"ALTER TABLE channels ADD COLUMN channel_proxy text",
		"ALTER TABLE sites ADD COLUMN proxy numeric NOT NULL DEFAULT 0",
		"ALTER TABLE sites ADD COLUMN site_proxy text",
		"ALTER TABLE sites ADD COLUMN use_system_proxy numeric NOT NULL DEFAULT 0",
		"ALTER TABLE site_accounts ADD COLUMN account_proxy text",
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatalf("add legacy proxy column: %v", err)
		}
	}

	channel := model.Channel{Name: "legacy-channel", ProxyMode: model.ProxyUsageModeDirect}
	site := model.Site{Name: "legacy-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://site.example", ProxyMode: model.ProxyUsageModeDirect}
	if err := database.Create(&channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := database.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{SiteID: site.ID, Name: "legacy-account", CredentialType: model.SiteCredentialTypeAPIKey, ProxyMode: model.ProxyUsageModeInherit}
	if err := database.Create(&account).Error; err != nil {
		t.Fatalf("create site account: %v", err)
	}
	proxyURL := "HTTP://Proxy.Example:8080"
	if err := database.Table("channels").Where("id = ?", channel.ID).Updates(map[string]any{"proxy": true, "channel_proxy": proxyURL}).Error; err != nil {
		t.Fatalf("seed channel proxy: %v", err)
	}
	if err := database.Table("sites").Where("id = ?", site.ID).Updates(map[string]any{"use_system_proxy": true}).Error; err != nil {
		t.Fatalf("seed site proxy: %v", err)
	}
	if err := database.Table("site_accounts").Where("id = ?", account.ID).Update("account_proxy", proxyURL).Error; err != nil {
		t.Fatalf("seed account proxy: %v", err)
	}

	if err := migrateProxyPool(database); err != nil {
		t.Fatalf("migrate legacy proxy schema: %v", err)
	}
	var proxyConfigurations []model.ProxyConfiguration
	if err := database.Find(&proxyConfigurations).Error; err != nil {
		t.Fatalf("load proxy configurations: %v", err)
	}
	if len(proxyConfigurations) != 1 {
		t.Fatalf("proxy configuration count = %d, want 1", len(proxyConfigurations))
	}
	if err := database.First(&channel, channel.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if channel.ProxyMode != model.ProxyUsageModePool || channel.ProxyConfigID == nil || *channel.ProxyConfigID != proxyConfigurations[0].ID {
		t.Fatalf("unexpected migrated channel proxy: %+v", channel)
	}
	if err := database.First(&site, site.ID).Error; err != nil {
		t.Fatalf("reload site: %v", err)
	}
	if site.ProxyMode != model.ProxyUsageModeSystem || site.ProxyConfigID != nil {
		t.Fatalf("unexpected migrated site proxy: %+v", site)
	}
	if err := database.First(&account, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if account.ProxyMode != model.ProxyUsageModePool || account.ProxyConfigID == nil || *account.ProxyConfigID != proxyConfigurations[0].ID {
		t.Fatalf("unexpected migrated account proxy: %+v", account)
	}
}

func TestMigrateProxyPoolRejectsNilDB(t *testing.T) {
	if err := migrateProxyPool(nil); err == nil {
		t.Fatal("expected nil database error")
	}
}

func openProxyMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(
		&model.ProxyConfiguration{},
		&model.Channel{},
		&model.Site{},
		&model.SiteAccount{},
	); err != nil {
		t.Fatalf("auto migrate proxy models: %v", err)
	}
	return database
}
