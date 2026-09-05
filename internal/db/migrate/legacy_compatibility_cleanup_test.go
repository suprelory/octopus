package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRemoveLegacyCompatibilityState(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:?_pragma=foreign_keys(ON)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	var foreignKeys int
	if err := database.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read sqlite foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("sqlite foreign_keys = %d, want 1", foreignKeys)
	}
	if err := database.AutoMigrate(
		&model.Setting{},
		&model.Group{},
		&model.GroupItem{},
		&model.GroupPreset{},
		&model.Channel{},
		&model.ChannelKey{},
		&model.StatsChannel{},
		&model.Site{},
		&model.SiteAccount{},
		&model.SiteToken{},
		&model.SiteUserGroup{},
		&model.SiteModel{},
		&model.SiteChannelBinding{},
	); err != nil {
		t.Fatalf("auto migrate current models: %v", err)
	}

	legacyColumns := []struct {
		table      string
		definition string
		column     string
	}{
		{table: "groups", definition: "`empty_response_detection` numeric", column: "empty_response_detection"},
		{table: "group_presets", definition: "`empty_response_detection` numeric", column: "empty_response_detection"},
		{table: "channels", definition: "`proxy` numeric", column: "proxy"},
		{table: "channels", definition: "`channel_proxy` text", column: "channel_proxy"},
		{table: "sites", definition: "`proxy` numeric", column: "proxy"},
		{table: "sites", definition: "`site_proxy` text", column: "site_proxy"},
		{table: "sites", definition: "`use_system_proxy` numeric", column: "use_system_proxy"},
		{table: "site_accounts", definition: "`account_proxy` text", column: "account_proxy"},
	}
	for _, legacyColumn := range legacyColumns {
		if err := database.Exec("ALTER TABLE " + legacyColumn.table + " ADD COLUMN " + legacyColumn.definition).Error; err != nil {
			t.Fatalf("add legacy column %s.%s: %v", legacyColumn.table, legacyColumn.column, err)
		}
	}
	group := model.Group{Name: "retained-group", Mode: model.GroupModeRoundRobin}
	if err := database.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	preset := model.GroupPreset{GroupID: group.ID, Name: "retained-preset", Mode: model.GroupModeRoundRobin}
	channel := model.Channel{Name: "retained-channel", ProxyMode: model.ProxyUsageModeDirect}
	site := model.Site{Name: "retained-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://site.example", ProxyMode: model.ProxyUsageModeDirect}
	for name, value := range map[string]any{
		"group preset": &preset,
		"channel":      &channel,
		"site":         &site,
	} {
		if err := database.Create(value).Error; err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "retained-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		ProxyMode:      model.ProxyUsageModeInherit,
	}
	if err := database.Create(&account).Error; err != nil {
		t.Fatalf("create site account: %v", err)
	}
	groupItem := model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o"}
	if err := database.Create(&groupItem).Error; err != nil {
		t.Fatalf("create group item: %v", err)
	}
	channelKey := model.ChannelKey{ChannelID: channel.ID, ChannelKey: "retained-key"}
	if err := database.Create(&channelKey).Error; err != nil {
		t.Fatalf("create channel key: %v", err)
	}
	statsChannel := model.StatsChannel{ChannelID: channel.ID}
	if err := database.Create(&statsChannel).Error; err != nil {
		t.Fatalf("create channel stats: %v", err)
	}
	siteToken := model.SiteToken{SiteAccountID: account.ID, Token: "retained-token"}
	if err := database.Create(&siteToken).Error; err != nil {
		t.Fatalf("create site token: %v", err)
	}
	siteUserGroup := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "default", Name: "default"}
	if err := database.Create(&siteUserGroup).Error; err != nil {
		t.Fatalf("create site user group: %v", err)
	}
	siteModel := model.SiteModel{SiteAccountID: account.ID, GroupKey: "default", ModelName: "gpt-4o"}
	if err := database.Create(&siteModel).Error; err != nil {
		t.Fatalf("create site model: %v", err)
	}
	siteBinding := model.SiteChannelBinding{
		SiteID: site.ID, SiteAccountID: account.ID, SiteUserGroupID: &siteUserGroup.ID,
		GroupKey: "default", ChannelID: channel.ID,
	}
	if err := database.Create(&siteBinding).Error; err != nil {
		t.Fatalf("create site channel binding: %v", err)
	}
	settings := []model.Setting{
		{Key: model.SettingKeyProjectedChannelAutoGroupEnabled, Value: " true "},
		{Key: model.SettingKey(legacySiteCheckinIntervalSetting), Value: "24"},
		{Key: model.SettingKeyProxyURL, Value: ""},
	}
	if err := database.Create(&settings).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := removeLegacyCompatibilityState(database); err != nil {
		t.Fatalf("removeLegacyCompatibilityState failed: %v", err)
	}
	if err := removeLegacyCompatibilityState(database); err != nil {
		t.Fatalf("removeLegacyCompatibilityState should be idempotent: %v", err)
	}

	for _, legacyColumn := range legacyColumns {
		if database.Migrator().HasColumn(legacyColumn.table, legacyColumn.column) {
			t.Errorf("legacy column %s.%s still exists", legacyColumn.table, legacyColumn.column)
		}
	}
	var autoGroup model.Setting
	if err := database.First(&autoGroup, "key = ?", model.SettingKeyProjectedChannelAutoGroupEnabled).Error; err != nil {
		t.Fatalf("reload projected auto-group setting: %v", err)
	}
	if autoGroup.Value != "1" {
		t.Fatalf("projected auto-group setting = %q, want 1", autoGroup.Value)
	}
	var legacySettingCount int64
	if err := database.Model(&model.Setting{}).Where("key = ?", legacySiteCheckinIntervalSetting).Count(&legacySettingCount).Error; err != nil {
		t.Fatalf("count legacy site check-in setting: %v", err)
	}
	if legacySettingCount != 0 {
		t.Fatalf("legacy site check-in setting count = %d, want 0", legacySettingCount)
	}
	for name, value := range map[string]any{
		"group":        &model.Group{ID: group.ID},
		"group preset": &model.GroupPreset{ID: preset.ID},
		"channel":      &model.Channel{ID: channel.ID},
		"site":         &model.Site{ID: site.ID},
		"site account": &model.SiteAccount{ID: account.ID},
	} {
		if err := database.First(value).Error; err != nil {
			t.Errorf("reload retained %s: %v", name, err)
		}
	}
	for name, value := range map[string]any{
		"group item":    &model.GroupItem{ID: groupItem.ID},
		"channel key":   &model.ChannelKey{ID: channelKey.ID},
		"channel stats": &model.StatsChannel{ChannelID: statsChannel.ChannelID},
		"site token":    &model.SiteToken{ID: siteToken.ID},
		"site group":    &model.SiteUserGroup{ID: siteUserGroup.ID},
		"site model":    &model.SiteModel{ID: siteModel.ID},
		"site binding":  &model.SiteChannelBinding{ID: siteBinding.ID},
	} {
		if err := database.First(value).Error; err != nil {
			t.Errorf("reload retained %s: %v", name, err)
		}
	}
	var foreignKeyViolations []struct {
		Table  string
		RowID  int `gorm:"column:rowid"`
		Parent string
		FKID   int `gorm:"column:fkid"`
	}
	if err := database.Raw("PRAGMA foreign_key_check").Scan(&foreignKeyViolations).Error; err != nil {
		t.Fatalf("check sqlite foreign keys: %v", err)
	}
	if len(foreignKeyViolations) != 0 {
		t.Fatalf("sqlite foreign key violations after cleanup: %+v", foreignKeyViolations)
	}
}

func TestRemoveLegacyCompatibilityStateRejectsNilDB(t *testing.T) {
	if err := removeLegacyCompatibilityState(nil); err == nil {
		t.Fatal("expected nil database error")
	}
}
