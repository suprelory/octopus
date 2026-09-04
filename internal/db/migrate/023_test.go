package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRemoveVolcengineChannelSupport(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(
		&model.Channel{},
		&model.ChannelKey{},
		&model.Site{},
		&model.SiteModel{},
		&model.SiteChannelBinding{},
		&model.Group{},
		&model.GroupItem{},
		&model.GroupPreset{},
		&model.WSResponseAffinity{},
	); err != nil {
		t.Fatalf("auto migrate models: %v", err)
	}

	legacyChannel := model.Channel{
		Name:    "legacy-volcengine",
		Type:    outbound.OutboundTypeUnsupported,
		Enabled: true,
	}
	activeChannel := model.Channel{
		Name:    "active-chat",
		Type:    outbound.OutboundTypeOpenAIChat,
		Enabled: true,
	}
	if err := database.Create(&[]*model.Channel{&legacyChannel, &activeChannel}).Error; err != nil {
		t.Fatalf("create channels: %v", err)
	}
	legacyKey := model.ChannelKey{ChannelID: legacyChannel.ID, Enabled: true, ChannelKey: "legacy-key"}
	activeKey := model.ChannelKey{ChannelID: activeChannel.ID, Enabled: true, ChannelKey: "active-key"}
	if err := database.Create(&[]*model.ChannelKey{&legacyKey, &activeKey}).Error; err != nil {
		t.Fatalf("create channel keys: %v", err)
	}
	affinities := []model.WSResponseAffinity{
		{
			APIKeyID:       1,
			GroupID:        1,
			RequestModel:   "legacy-model",
			ResponseIDHash: "legacy-response",
			ChannelID:      legacyChannel.ID,
			ChannelKeyID:   legacyKey.ID,
			ExpiresAt:      time.Now().Add(time.Hour),
		},
		{
			APIKeyID:       1,
			GroupID:        1,
			RequestModel:   "active-model",
			ResponseIDHash: "active-response",
			ChannelID:      activeChannel.ID,
			ChannelKeyID:   activeKey.ID,
			ExpiresAt:      time.Now().Add(time.Hour),
		},
	}
	if err := database.Create(&affinities).Error; err != nil {
		t.Fatalf("create websocket response affinities: %v", err)
	}

	site := model.Site{
		Name:             "legacy-site",
		Platform:         model.SitePlatformNewAPI,
		BaseURL:          "https://site.example",
		DefaultRouteType: model.SiteModelRouteType("volcengine"),
		RouteBaseURLs: []model.SiteRouteBaseURL{
			{RouteType: model.SiteModelRouteType("volcengine"), BaseURL: "https://site.example/ark"},
			{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://site.example/anthropic"},
		},
	}
	if err := database.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}

	legacyMetadata := `{"kind":"site_route_metadata","version":1,"source":"/api/models","route_supported":true,"route_type":"volcengine","supported_endpoint_types":["ark"]}`
	legacyModel := model.SiteModel{
		SiteAccountID:   1,
		GroupKey:        model.SiteDefaultGroupKey,
		ModelName:       "doubao-seed-1-6",
		RouteType:       model.SiteModelRouteType("volcengine"),
		RouteRawPayload: legacyMetadata,
	}
	activeModel := model.SiteModel{
		SiteAccountID: 2,
		GroupKey:      model.SiteDefaultGroupKey,
		ModelName:     "gpt-4o",
		RouteType:     model.SiteModelRouteTypeOpenAIChat,
	}
	manualModel := model.SiteModel{
		SiteAccountID:  3,
		GroupKey:       model.SiteDefaultGroupKey,
		ModelName:      "doubao-openai-compatible",
		RouteType:      model.SiteModelRouteTypeAnthropic,
		RouteSource:    model.SiteModelRouteSourceManualOverride,
		ManualOverride: true,
	}
	if err := database.Create(&[]*model.SiteModel{&legacyModel, &activeModel, &manualModel}).Error; err != nil {
		t.Fatalf("create site models: %v", err)
	}

	group := model.Group{Name: "legacy-group", Mode: model.GroupModeRoundRobin}
	if err := database.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	bindings := []model.SiteChannelBinding{
		{SiteID: site.ID, SiteAccountID: 1, GroupKey: "legacy", ChannelID: legacyChannel.ID},
		{SiteID: site.ID, SiteAccountID: 2, GroupKey: "active", ChannelID: activeChannel.ID},
	}
	if err := database.Create(&bindings).Error; err != nil {
		t.Fatalf("create site channel bindings: %v", err)
	}
	items := []model.GroupItem{
		{GroupID: group.ID, ChannelID: legacyChannel.ID, ModelName: "doubao-seed-1-6"},
		{GroupID: group.ID, ChannelID: activeChannel.ID, ModelName: "gpt-4o"},
	}
	if err := database.Create(&items).Error; err != nil {
		t.Fatalf("create group items: %v", err)
	}
	preset := model.GroupPreset{
		GroupID: group.ID,
		Name:    "legacy-preset",
		Mode:    model.GroupModeRoundRobin,
		Items: []model.GroupPresetItem{
			{ChannelID: legacyChannel.ID, ModelName: "doubao-seed-1-6", Priority: 1},
			{ChannelID: activeChannel.ID, ModelName: "gpt-4o", Priority: 2},
		},
	}
	if err := database.Create(&preset).Error; err != nil {
		t.Fatalf("create group preset: %v", err)
	}

	if err := removeVolcengineChannelSupport(database); err != nil {
		t.Fatalf("removeVolcengineChannelSupport failed: %v", err)
	}
	if err := removeVolcengineChannelSupport(database); err != nil {
		t.Fatalf("removeVolcengineChannelSupport should be idempotent: %v", err)
	}

	var migratedLegacy model.Channel
	if err := database.First(&migratedLegacy, legacyChannel.ID).Error; err != nil {
		t.Fatalf("reload legacy channel: %v", err)
	}
	if migratedLegacy.Type != outbound.OutboundTypeUnsupported || migratedLegacy.Enabled {
		t.Fatalf("expected legacy channel to be retained disabled, got type=%d enabled=%t", migratedLegacy.Type, migratedLegacy.Enabled)
	}
	var migratedActive model.Channel
	if err := database.First(&migratedActive, activeChannel.ID).Error; err != nil {
		t.Fatalf("reload active channel: %v", err)
	}
	if !migratedActive.Enabled {
		t.Fatal("expected supported channel to remain enabled")
	}
	var migratedLegacyKey model.ChannelKey
	if err := database.First(&migratedLegacyKey, legacyKey.ID).Error; err != nil {
		t.Fatalf("reload legacy channel key: %v", err)
	}
	if migratedLegacyKey.Enabled {
		t.Fatal("expected legacy channel key to be disabled")
	}
	var migratedActiveKey model.ChannelKey
	if err := database.First(&migratedActiveKey, activeKey.ID).Error; err != nil {
		t.Fatalf("reload active channel key: %v", err)
	}
	if !migratedActiveKey.Enabled {
		t.Fatal("expected supported channel key to remain enabled")
	}

	var migratedSite model.Site
	if err := database.First(&migratedSite, site.ID).Error; err != nil {
		t.Fatalf("reload site: %v", err)
	}
	if migratedSite.DefaultRouteType != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected removed default route to become chat, got %q", migratedSite.DefaultRouteType)
	}
	if len(migratedSite.RouteBaseURLs) != 1 || migratedSite.RouteBaseURLs[0].RouteType != model.SiteModelRouteTypeAnthropic {
		t.Fatalf("expected legacy base URL override to be removed, got %+v", migratedSite.RouteBaseURLs)
	}

	var migratedModel model.SiteModel
	if err := database.First(&migratedModel, legacyModel.ID).Error; err != nil {
		t.Fatalf("reload legacy model: %v", err)
	}
	if migratedModel.RouteType != model.SiteModelRouteTypeUnknown {
		t.Fatalf("expected legacy model route to become unknown, got %q", migratedModel.RouteType)
	}
	metadata, ok := model.ParseSiteModelRouteMetadata(migratedModel.RouteRawPayload)
	if !ok || metadata.RouteSupported || metadata.RouteType != model.SiteModelRouteTypeUnknown {
		t.Fatalf("expected unsupported route metadata, got ok=%t metadata=%+v", ok, metadata)
	}
	var migratedManual model.SiteModel
	if err := database.First(&migratedManual, manualModel.ID).Error; err != nil {
		t.Fatalf("reload manually routed model: %v", err)
	}
	if migratedManual.RouteType != model.SiteModelRouteTypeAnthropic || !migratedManual.ManualOverride {
		t.Fatalf("expected supported manual route to remain intact, got %+v", migratedManual)
	}

	var bindingCount int64
	if err := database.Model(&model.SiteChannelBinding{}).Where("channel_id = ?", legacyChannel.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count legacy bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("expected legacy bindings to be removed, got %d", bindingCount)
	}
	if err := database.Model(&model.GroupItem{}).Where("channel_id = ?", legacyChannel.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count legacy group items: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("expected legacy group items to be removed, got %d", bindingCount)
	}
	if err := database.Model(&model.SiteChannelBinding{}).Where("channel_id = ?", activeChannel.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count active bindings: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("expected supported binding to remain, got %d", bindingCount)
	}
	if err := database.Model(&model.WSResponseAffinity{}).Where("channel_id = ?", legacyChannel.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count legacy websocket response affinities: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("expected legacy websocket response affinities to be removed, got %d", bindingCount)
	}
	if err := database.Model(&model.WSResponseAffinity{}).Where("channel_id = ?", activeChannel.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count active websocket response affinities: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("expected supported websocket response affinity to remain, got %d", bindingCount)
	}
	var migratedPreset model.GroupPreset
	if err := database.First(&migratedPreset, preset.ID).Error; err != nil {
		t.Fatalf("reload migrated group preset: %v", err)
	}
	if len(migratedPreset.Items) != 1 || migratedPreset.Items[0].ChannelID != activeChannel.ID {
		t.Fatalf("expected legacy preset item to be removed, got %+v", migratedPreset.Items)
	}
}

func TestRemoveVolcengineChannelSupportRejectsNilDB(t *testing.T) {
	if err := removeVolcengineChannelSupport(nil); err == nil {
		t.Fatal("expected nil database error")
	}
}
