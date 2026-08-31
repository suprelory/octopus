package op

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func setupBackupTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-backup-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func TestDBImportPreservesAllAccountsOnCleanDB(t *testing.T) {
	ctx := setupBackupTestDB(t)

	dump := buildTestDump()
	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	if result.RowsAffected["sites"] != 1 {
		t.Fatalf("expected 1 site created, got %d", result.RowsAffected["sites"])
	}
	if result.RowsAffected["site_accounts"] != 3 {
		t.Fatalf("expected 3 site accounts created, got %d", result.RowsAffected["site_accounts"])
	}

	site, err := SiteGet(1, ctx)
	if err != nil {
		// Site might have a different ID after import; query by platform+url
		var sites []model.Site
		if qerr := dbpkg.GetDB().Where("platform = ? AND base_url = ?", "new-api", "https://example.com").Find(&sites).Error; qerr != nil {
			t.Fatalf("query sites failed: %v", qerr)
		}
		if len(sites) != 1 {
			t.Fatalf("expected 1 site, got %d", len(sites))
		}
		site, err = SiteGet(sites[0].ID, ctx)
		if err != nil {
			t.Fatalf("SiteGet failed: %v", err)
		}
	}
	if len(site.Accounts) != 3 {
		t.Fatalf("expected site to have 3 accounts, got %d", len(site.Accounts))
	}
}

func TestDBImportWithIDCollisionPreservesAllAccounts(t *testing.T) {
	ctx := setupBackupTestDB(t)

	// Create pre-existing data that will cause ID collisions
	preexistingSite := &model.Site{
		Name:     "other-site",
		Platform: model.SitePlatformOneAPI,
		BaseURL:  "https://other.com",
		Enabled:  true,
	}
	if err := SiteCreate(preexistingSite, ctx); err != nil {
		t.Fatalf("create pre-existing site failed: %v", err)
	}
	preexistingAccount := &model.SiteAccount{
		SiteID:         preexistingSite.ID,
		Name:           "other-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "sk-other",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := SiteAccountCreate(preexistingAccount, ctx); err != nil {
		t.Fatalf("create pre-existing account failed: %v", err)
	}

	// Now import a dump that has records with IDs that overlap
	dump := buildTestDump()
	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	// All 3 accounts from the dump should be imported
	if result.RowsAffected["site_accounts"] != 3 {
		t.Fatalf("expected 3 site accounts created, got %d", result.RowsAffected["site_accounts"])
	}

	// The pre-existing data should still be intact
	var totalAccounts int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&totalAccounts).Error; err != nil {
		t.Fatalf("count accounts failed: %v", err)
	}
	if totalAccounts != 4 { // 1 pre-existing + 3 imported
		t.Fatalf("expected 4 total accounts, got %d", totalAccounts)
	}

	// Verify the imported site has all 3 accounts
	var importedSite model.Site
	if err := dbpkg.GetDB().Where("platform = ? AND base_url = ?", "new-api", "https://example.com").First(&importedSite).Error; err != nil {
		t.Fatalf("query imported site failed: %v", err)
	}
	var importedAccountCount int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Where("site_id = ?", importedSite.ID).Count(&importedAccountCount).Error; err != nil {
		t.Fatalf("count imported accounts failed: %v", err)
	}
	if importedAccountCount != 3 {
		t.Fatalf("expected imported site to have 3 accounts, got %d", importedAccountCount)
	}
}

func TestDBImportDeduplicatesOnSecondImport(t *testing.T) {
	ctx := setupBackupTestDB(t)

	dump := buildTestDump()

	// First import
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("first DBImportIncremental failed: %v", err)
	}

	// Second import of the same data
	dump2 := buildTestDump()
	result, err := DBImportIncremental(ctx, dump2)
	if err != nil {
		t.Fatalf("second DBImportIncremental failed: %v", err)
	}

	// Nothing new should be created (all deduped)
	if result.RowsAffected["sites"] != 0 {
		t.Fatalf("expected 0 new sites on second import, got %d", result.RowsAffected["sites"])
	}
	if result.RowsAffected["site_accounts"] != 0 {
		t.Fatalf("expected 0 new accounts on second import, got %d", result.RowsAffected["site_accounts"])
	}
	if result.RowsAffected["channels"] != 0 {
		t.Fatalf("expected 0 new channels on second import, got %d", result.RowsAffected["channels"])
	}

	// Total counts should remain the same
	var siteCount, accountCount, channelCount int64
	dbpkg.GetDB().Model(&model.Site{}).Count(&siteCount)
	dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&accountCount)
	dbpkg.GetDB().Model(&model.Channel{}).Count(&channelCount)

	if siteCount != 1 {
		t.Fatalf("expected 1 site after double import, got %d", siteCount)
	}
	if accountCount != 3 {
		t.Fatalf("expected 3 accounts after double import, got %d", accountCount)
	}
	if channelCount != 1 {
		t.Fatalf("expected 1 channel after double import, got %d", channelCount)
	}
}

func TestDBImportSkipsOrphanedStats(t *testing.T) {
	ctx := setupBackupTestDB(t)

	dump := buildTestDump()
	dump.IncludeStats = true
	dump.StatsChannel = []model.StatsChannel{
		{ChannelID: 1, StatsMetrics: model.StatsMetrics{RequestSuccess: 1}},
		{ChannelID: 999, StatsMetrics: model.StatsMetrics{RequestSuccess: 2}},
	}
	dump.StatsModel = []model.StatsModel{
		{ID: 1, ChannelID: 1, Name: "gpt-4", StatsMetrics: model.StatsMetrics{RequestSuccess: 1}},
		{ID: 2, ChannelID: 999, Name: "orphan", StatsMetrics: model.StatsMetrics{RequestSuccess: 2}},
	}
	dump.StatsAPIKey = []model.StatsAPIKey{
		{APIKeyID: 999, StatsMetrics: model.StatsMetrics{RequestSuccess: 2}},
	}

	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}
	if result.RowsAffected["stats_channel"] != 1 {
		t.Fatalf("expected 1 stats_channel imported, got %d", result.RowsAffected["stats_channel"])
	}
	if result.RowsAffected["stats_model"] != 1 {
		t.Fatalf("expected 1 stats_model imported, got %d", result.RowsAffected["stats_model"])
	}
	if result.RowsAffected["stats_api_key"] != 0 {
		t.Fatalf("expected 0 stats_api_key imported, got %d", result.RowsAffected["stats_api_key"])
	}
}

func TestDBExportThenImportRoundtrip(t *testing.T) {
	ctx := setupBackupTestDB(t)

	// Create test data
	site := &model.Site{
		Name:     "roundtrip-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://roundtrip.example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		account := &model.SiteAccount{
			SiteID:         site.ID,
			Name:           mustSprintf("account-%d", i),
			CredentialType: model.SiteCredentialTypeAPIKey,
			APIKey:         mustSprintf("sk-key-%d", i),
			Enabled:        true,
			AutoSync:       true,
		}
		if err := SiteAccountCreate(account, ctx); err != nil {
			t.Fatalf("SiteAccountCreate failed: %v", err)
		}
	}

	// Export
	dump, err := DBExportAll(ctx, false, false)
	if err != nil {
		t.Fatalf("DBExportAll failed: %v", err)
	}

	// Verify export contains all accounts
	if len(dump.SiteAccounts) != 5 {
		t.Fatalf("expected 5 accounts in export, got %d", len(dump.SiteAccounts))
	}

	// Close and re-create a fresh DB
	_ = dbpkg.Close()
	freshDBPath := filepath.Join(t.TempDir(), "octopus-fresh.db")
	if err := dbpkg.InitDB("sqlite", freshDBPath, false); err != nil {
		t.Fatalf("InitDB for fresh DB failed: %v", err)
	}

	// Import to fresh DB
	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental to fresh DB failed: %v", err)
	}
	if result.RowsAffected["sites"] != 1 {
		t.Fatalf("expected 1 site imported, got %d", result.RowsAffected["sites"])
	}
	if result.RowsAffected["site_accounts"] != 5 {
		t.Fatalf("expected 5 accounts imported, got %d", result.RowsAffected["site_accounts"])
	}

	// Verify all accounts are present
	var freshSite model.Site
	if err := dbpkg.GetDB().Where("platform = ? AND base_url = ?", "new-api", "https://roundtrip.example.com").First(&freshSite).Error; err != nil {
		t.Fatalf("query imported site failed: %v", err)
	}
	var accountCount int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Where("site_id = ?", freshSite.ID).Count(&accountCount).Error; err != nil {
		t.Fatalf("count accounts failed: %v", err)
	}
	if accountCount != 5 {
		t.Fatalf("expected 5 accounts for imported site, got %d", accountCount)
	}
}

func buildTestDump() *model.DBDump {
	return &model.DBDump{
		Version:      1,
		IncludeLogs:  false,
		IncludeStats: false,
		Channels: []model.Channel{
			{ID: 1, Name: "test-channel", Enabled: true},
		},
		ChannelKeys: []model.ChannelKey{
			{ID: 1, ChannelID: 1, Enabled: true, ChannelKey: "sk-chan-1"},
		},
		Sites: []model.Site{
			{ID: 1, Name: "test-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com", Enabled: true},
		},
		SiteAccounts: []model.SiteAccount{
			{ID: 1, SiteID: 1, Name: "account-1", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-1", Enabled: true, AutoSync: true},
			{ID: 2, SiteID: 1, Name: "account-2", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-2", Enabled: true, AutoSync: true},
			{ID: 3, SiteID: 1, Name: "account-3", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-3", Enabled: true, AutoSync: true},
		},
		Groups: []model.Group{
			{ID: 1, Name: "test-group", Mode: 0},
		},
		GroupItems: []model.GroupItem{
			{ID: 1, GroupID: 1, ChannelID: 1, ModelName: "gpt-4", Priority: 1, Weight: 1},
		},
	}
}

func TestDBImportRetiresLegacyChannelReferences(t *testing.T) {
	ctx := setupBackupTestDB(t)
	dump := &model.DBDump{
		Version:      1,
		IncludeStats: true,
		Channels: []model.Channel{{
			ID:      41,
			Name:    "backup-legacy-volcengine",
			Type:    outbound.OutboundTypeUnsupported,
			Enabled: true,
		}},
		ChannelKeys: []model.ChannelKey{{
			ID:         51,
			ChannelID:  41,
			Enabled:    true,
			ChannelKey: "legacy-secret",
		}},
		Groups: []model.Group{{ID: 61, Name: "backup-legacy-group", Mode: model.GroupModeRoundRobin}},
		GroupItems: []model.GroupItem{{
			ID:        71,
			GroupID:   61,
			ChannelID: 41,
			ModelName: "doubao-seed-1-6",
		}},
		StatsChannel: []model.StatsChannel{{
			ChannelID:    41,
			StatsMetrics: model.StatsMetrics{RequestSuccess: 3},
		}},
	}

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	var channel model.Channel
	if err := dbpkg.GetDB().WithContext(ctx).Where("name = ?", "backup-legacy-volcengine").First(&channel).Error; err != nil {
		t.Fatalf("reload imported channel failed: %v", err)
	}
	if channel.Enabled {
		t.Fatal("legacy channel must remain disabled after import")
	}

	var keyCount, itemCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.ChannelKey{}).Where("channel_id = ?", channel.ID).Count(&keyCount).Error; err != nil {
		t.Fatalf("count imported legacy keys failed: %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("expected legacy channel keys to be skipped, got %d", keyCount)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("channel_id = ?", channel.ID).Count(&itemCount).Error; err != nil {
		t.Fatalf("count imported legacy group items failed: %v", err)
	}
	if itemCount != 0 {
		t.Fatalf("expected legacy group items to be skipped, got %d", itemCount)
	}

	var statsCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.StatsChannel{}).Where("channel_id = ?", channel.ID).Count(&statsCount).Error; err != nil {
		t.Fatalf("count imported legacy stats failed: %v", err)
	}
	if statsCount != 1 {
		t.Fatalf("expected legacy statistics to be retained, got %d", statsCount)
	}
}

func TestDBImportSkipsReferencesToExistingUnsupportedChannel(t *testing.T) {
	ctx := setupBackupTestDB(t)
	legacy := model.Channel{Name: "existing-legacy-volcengine", Type: outbound.OutboundTypeUnsupported, Enabled: false}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&legacy).Error; err != nil {
		t.Fatalf("create existing legacy channel failed: %v", err)
	}

	dump := &model.DBDump{
		Version:             1,
		IncludeStats:        true,
		ChannelKeys:         []model.ChannelKey{{ID: 1, ChannelID: legacy.ID, Enabled: true, ChannelKey: "legacy-secret"}},
		Sites:               []model.Site{{ID: 11, Name: "orphan-ref-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://orphan.example", Enabled: true}},
		SiteAccounts:        []model.SiteAccount{{ID: 12, SiteID: 11, Name: "orphan-ref-account", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-test", Enabled: true}},
		SiteChannelBindings: []model.SiteChannelBinding{{ID: 13, SiteID: 11, SiteAccountID: 12, GroupKey: model.SiteDefaultGroupKey, ChannelID: legacy.ID}},
		Groups:              []model.Group{{ID: 14, Name: "orphan-ref-group", Mode: model.GroupModeRoundRobin}},
		GroupItems:          []model.GroupItem{{ID: 15, GroupID: 14, ChannelID: legacy.ID, ModelName: "doubao-seed-1-6"}},
		StatsChannel:        []model.StatsChannel{{ChannelID: legacy.ID, StatsMetrics: model.StatsMetrics{RequestSuccess: 2}}},
		StatsModel:          []model.StatsModel{{ID: 16, ChannelID: legacy.ID, Name: "doubao-seed-1-6", StatsMetrics: model.StatsMetrics{RequestSuccess: 2}}},
	}

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	for table, row := range map[string]any{
		"channel keys":          &model.ChannelKey{},
		"site channel bindings": &model.SiteChannelBinding{},
		"group items":           &model.GroupItem{},
	} {
		var count int64
		if err := dbpkg.GetDB().WithContext(ctx).Model(row).Where("channel_id = ?", legacy.ID).Count(&count).Error; err != nil {
			t.Fatalf("count %s failed: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected unsupported %s to be skipped, got %d", table, count)
		}
	}

	var statsChannelCount, statsModelCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.StatsChannel{}).Where("channel_id = ?", legacy.ID).Count(&statsChannelCount).Error; err != nil {
		t.Fatalf("count stats_channel failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.StatsModel{}).Where("channel_id = ?", legacy.ID).Count(&statsModelCount).Error; err != nil {
		t.Fatalf("count stats_model failed: %v", err)
	}
	if statsChannelCount != 1 || statsModelCount != 1 {
		t.Fatalf("expected historical stats to remain, got channel=%d model=%d", statsChannelCount, statsModelCount)
	}
}

func TestDBImportDoesNotAttachLegacyStatsToSameNamedSupportedChannel(t *testing.T) {
	ctx := setupBackupTestDB(t)
	existing := model.Channel{Name: "shared-channel-name", Type: outbound.OutboundTypeOpenAIChat, Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&existing).Error; err != nil {
		t.Fatalf("create existing supported channel failed: %v", err)
	}

	dump := &model.DBDump{
		Version:      1,
		IncludeStats: true,
		Channels: []model.Channel{{
			ID:      91,
			Name:    "shared-channel-name",
			Type:    outbound.OutboundTypeUnsupported,
			Enabled: true,
		}},
		StatsChannel: []model.StatsChannel{{
			ChannelID:    91,
			StatsMetrics: model.StatsMetrics{RequestSuccess: 7},
		}},
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	var channels []model.Channel
	if err := dbpkg.GetDB().WithContext(ctx).Where("name LIKE ?", "shared-channel-name%").Order("id ASC").Find(&channels).Error; err != nil {
		t.Fatalf("query imported channels failed: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("expected supported and retired channels to remain distinct, got %+v", channels)
	}
	var retired model.Channel
	for _, channel := range channels {
		if channel.ID != existing.ID {
			retired = channel
		}
	}
	if retired.Type != outbound.OutboundTypeUnsupported || retired.Enabled {
		t.Fatalf("expected renamed retired channel to stay disabled, got %+v", retired)
	}
	if existing.Type != outbound.OutboundTypeOpenAIChat || !existing.Enabled {
		t.Fatalf("expected existing supported channel to remain unchanged, got %+v", existing)
	}

	var retiredStats, existingStats int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.StatsChannel{}).Where("channel_id = ?", retired.ID).Count(&retiredStats).Error; err != nil {
		t.Fatalf("count retired channel stats failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.StatsChannel{}).Where("channel_id = ?", existing.ID).Count(&existingStats).Error; err != nil {
		t.Fatalf("count existing channel stats failed: %v", err)
	}
	if retiredStats != 1 || existingStats != 0 {
		t.Fatalf("expected stats to stay on retired channel, got retired=%d existing=%d", retiredStats, existingStats)
	}
}

func TestDBImportNormalizesRemovedSiteRoutes(t *testing.T) {
	ctx := setupBackupTestDB(t)
	legacyMetadata := `{"kind":"site_route_metadata","version":1,"route_supported":true,"route_type":"volcengine","supported_endpoint_types":["ark"]}`
	dump := &model.DBDump{
		Version: 1,
		Sites: []model.Site{{
			ID:               21,
			Name:             "legacy-route-site",
			Platform:         model.SitePlatformNewAPI,
			BaseURL:          "https://legacy-route.example",
			Enabled:          true,
			DefaultRouteType: model.SiteModelRouteType("volcengine"),
			RouteBaseURLs: []model.SiteRouteBaseURL{
				{RouteType: model.SiteModelRouteType("ark"), BaseURL: "https://legacy-route.example/ark"},
				{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://legacy-route.example/anthropic"},
			},
		}},
		SiteAccounts: []model.SiteAccount{{
			ID: 22, SiteID: 21, Name: "legacy-route-account", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-test", Enabled: true,
		}},
		SiteModels: []model.SiteModel{
			{ID: 23, SiteAccountID: 22, GroupKey: model.SiteDefaultGroupKey, ModelName: "doubao-seed-1-6", RouteType: model.SiteModelRouteType("volcengine"), RouteRawPayload: legacyMetadata},
			{ID: 24, SiteAccountID: 22, GroupKey: model.SiteDefaultGroupKey, ModelName: "doubao-openai-compatible", RouteType: model.SiteModelRouteTypeAnthropic, RouteSource: model.SiteModelRouteSourceManualOverride, ManualOverride: true},
			{ID: 25, SiteAccountID: 22, GroupKey: model.SiteDefaultGroupKey, ModelName: "legacy-ark-model", RouteType: model.SiteModelRouteType("ark"), RouteSource: model.SiteModelRouteSourceManualOverride, ManualOverride: true},
		},
	}

	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	var importedSite model.Site
	if err := dbpkg.GetDB().WithContext(ctx).Where("name = ?", "legacy-route-site").First(&importedSite).Error; err != nil {
		t.Fatalf("reload imported site failed: %v", err)
	}
	if importedSite.DefaultRouteType != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected removed default route to become chat, got %q", importedSite.DefaultRouteType)
	}
	if len(importedSite.RouteBaseURLs) != 1 || importedSite.RouteBaseURLs[0].RouteType != model.SiteModelRouteTypeAnthropic {
		t.Fatalf("expected removed route overrides to be dropped, got %+v", importedSite.RouteBaseURLs)
	}

	var rows []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id IN (SELECT id FROM site_accounts WHERE site_id = ?)", importedSite.ID).Order("model_name ASC").Find(&rows).Error; err != nil {
		t.Fatalf("reload imported site models failed: %v", err)
	}
	byName := make(map[string]model.SiteModel, len(rows))
	for _, row := range rows {
		byName[row.ModelName] = row
	}
	for _, name := range []string{"doubao-seed-1-6", "legacy-ark-model"} {
		row := byName[name]
		if row.RouteType != model.SiteModelRouteTypeUnknown || row.ManualOverride {
			t.Fatalf("expected removed route %q to be unsupported, got %+v", name, row)
		}
		metadata, ok := model.ParseSiteModelRouteMetadata(row.RouteRawPayload)
		if !ok || metadata.RouteSupported {
			t.Fatalf("expected removed route metadata for %q, got ok=%t metadata=%+v", name, ok, metadata)
		}
	}
	manual := byName["doubao-openai-compatible"]
	if manual.RouteType != model.SiteModelRouteTypeAnthropic || !manual.ManualOverride {
		t.Fatalf("expected supported manual route to remain intact, got %+v", manual)
	}
}

func mustSprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func TestDBExportZipContainsRelayLogsNDJSON(t *testing.T) {
	ctx := setupBackupTestDB(t)

	if err := dbpkg.GetDB().Create(&[]model.RelayLog{
		{ID: 1001, Time: 1, RequestModelName: "a", Success: true},
		{ID: 1002, Time: 2, RequestModelName: "b", Success: true},
	}).Error; err != nil {
		t.Fatalf("seed relay logs failed: %v", err)
	}

	var buf bytesBuffer
	if err := DBExportZip(ctx, &buf, true, false); err != nil {
		t.Fatalf("DBExportZip failed: %v", err)
	}

	zr, err := zipReaderFromBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("zip open failed: %v", err)
	}
	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, required := range []string{"manifest.json", "channels.json", "relay_logs.ndjson"} {
		if !names[required] {
			t.Fatalf("zip missing %q (have %v)", required, names)
		}
	}

	ndjson := readZipFile(t, zr, "relay_logs.ndjson")
	if ndjson == "" {
		t.Fatalf("relay_logs.ndjson is empty")
	}
	if linesCount(ndjson) != 2 {
		t.Fatalf("expected 2 ndjson lines, got %d (%q)", linesCount(ndjson), ndjson)
	}
}
