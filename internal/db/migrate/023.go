package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 23,
		Up:      removeVolcengineChannelSupport,
	})
}

// removeVolcengineChannelSupport retires persisted Volcengine channels and
// route metadata without renumbering the remaining outbound types. Historical
// rows are retained where they are useful for audit/statistics, but no active
// binding or group item can continue to select the removed adapter.
func removeVolcengineChannelSupport(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	unsupportedChannelIDs := make([]int, 0)
	if db.Migrator().HasTable(&model.Channel{}) {
		var channels []model.Channel
		if err := db.Select("id", "enabled").Where("type = ?", outbound.OutboundTypeUnsupported).Find(&channels).Error; err != nil {
			return fmt.Errorf("list legacy Volcengine channels: %w", err)
		}
		for _, channel := range channels {
			unsupportedChannelIDs = append(unsupportedChannelIDs, channel.ID)
		}
		if len(unsupportedChannelIDs) > 0 {
			if err := db.Model(&model.Channel{}).
				Where("type = ?", outbound.OutboundTypeUnsupported).
				Update("enabled", false).Error; err != nil {
				return fmt.Errorf("disable legacy Volcengine channels: %w", err)
			}
		}
	}

	if db.Migrator().HasTable(&model.SiteModel{}) {
		var siteModels []model.SiteModel
		if err := db.Find(&siteModels).Error; err != nil {
			return fmt.Errorf("list site models for legacy route cleanup: %w", err)
		}
		for _, item := range siteModels {
			updates := make(map[string]any)
			legacyRoute := model.IsRemovedSiteModelRouteType(item.RouteType)
			metadata, hasMetadata := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
			legacyMetadata := model.ContainsRemovedSiteModelRouteMarker(item.RouteRawPayload) ||
				hasMetadata && isRemovedRouteMetadata(metadata)
			manualSupportedRoute := item.ManualOverride &&
				!legacyRoute &&
				!legacyMetadata &&
				model.IsProjectedSiteModelRouteType(model.NormalizeSiteModelRouteType(item.RouteType))
			legacyModel := model.ContainsRemovedSiteModelRouteMarker(item.ModelName) && !manualSupportedRoute
			if legacyRoute || legacyModel || legacyMetadata {
				updates["route_type"] = model.SiteModelRouteTypeUnknown
				updates["route_source"] = model.SiteModelRouteSourceSyncInferred
				updates["manual_override"] = false
				if !hasMetadata {
					metadata = &model.SiteModelRouteMetadata{}
				}
				metadata.RouteSupported = false
				metadata.RouteGuessed = false
				metadata.RouteType = model.SiteModelRouteTypeUnknown
				if strings.TrimSpace(metadata.UnsupportedReason) == "" {
					metadata.UnsupportedReason = "Volcengine/Ark route support has been removed"
				}
				normalized := metadata.Marshal()
				if strings.TrimSpace(normalized) != strings.TrimSpace(item.RouteRawPayload) {
					updates["route_raw_payload"] = normalized
				}
			}
			if len(updates) == 0 {
				continue
			}
			if err := db.Model(&model.SiteModel{}).Where("id = ?", item.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("normalize legacy site model route %d: %w", item.ID, err)
			}
		}
	}

	if db.Migrator().HasTable(&model.Site{}) {
		var sites []model.Site
		if err := db.Find(&sites).Error; err != nil {
			return fmt.Errorf("list sites for legacy route cleanup: %w", err)
		}
		for _, site := range sites {
			updates := model.Site{ID: site.ID}
			selectFields := make([]string, 0, 2)
			if model.IsRemovedSiteModelRouteType(site.DefaultRouteType) {
				updates.DefaultRouteType = model.SiteModelRouteTypeOpenAIChat
				selectFields = append(selectFields, "default_route_type")
			}
			normalizedBaseURLs := model.NormalizeSiteRouteBaseURLs(site.RouteBaseURLs)
			if len(normalizedBaseURLs) != len(site.RouteBaseURLs) {
				updates.RouteBaseURLs = normalizedBaseURLs
				selectFields = append(selectFields, "route_base_urls")
			}
			if len(selectFields) == 0 {
				continue
			}
			if err := db.Model(&model.Site{}).Where("id = ?", site.ID).Select(selectFields).Updates(&updates).Error; err != nil {
				return fmt.Errorf("normalize legacy site route %d: %w", site.ID, err)
			}
		}
	}

	if len(unsupportedChannelIDs) > 0 && db.Migrator().HasTable(&model.SiteChannelBinding{}) {
		if err := db.Where("channel_id IN ?", unsupportedChannelIDs).Delete(&model.SiteChannelBinding{}).Error; err != nil {
			return fmt.Errorf("delete legacy site channel bindings: %w", err)
		}
	}
	if len(unsupportedChannelIDs) > 0 && db.Migrator().HasTable(&model.WSResponseAffinity{}) {
		if err := db.Where("channel_id IN ?", unsupportedChannelIDs).Delete(&model.WSResponseAffinity{}).Error; err != nil {
			return fmt.Errorf("delete legacy websocket response affinities: %w", err)
		}
	}
	if len(unsupportedChannelIDs) > 0 && db.Migrator().HasTable(&model.ChannelKey{}) {
		// Keep key rows for audit/history, but make sure a legacy credential can
		// never become usable if an old channel record is inspected directly.
		if err := db.Model(&model.ChannelKey{}).
			Where("channel_id IN ?", unsupportedChannelIDs).
			Update("enabled", false).Error; err != nil {
			return fmt.Errorf("disable legacy channel keys: %w", err)
		}
	}
	if len(unsupportedChannelIDs) > 0 && db.Migrator().HasTable(&model.GroupItem{}) {
		if err := db.Where("channel_id IN ?", unsupportedChannelIDs).Delete(&model.GroupItem{}).Error; err != nil {
			return fmt.Errorf("delete legacy group items: %w", err)
		}
	}
	if len(unsupportedChannelIDs) > 0 && db.Migrator().HasTable(&model.GroupPreset{}) {
		removed := make(map[int]struct{}, len(unsupportedChannelIDs))
		for _, id := range unsupportedChannelIDs {
			removed[id] = struct{}{}
		}
		var presets []model.GroupPreset
		if err := db.Find(&presets).Error; err != nil {
			return fmt.Errorf("list group presets for legacy channel cleanup: %w", err)
		}
		for _, preset := range presets {
			if len(preset.Items) == 0 {
				continue
			}
			filtered := make([]model.GroupPresetItem, 0, len(preset.Items))
			changed := false
			for _, item := range preset.Items {
				if _, ok := removed[item.ChannelID]; ok {
					changed = true
					continue
				}
				filtered = append(filtered, item)
			}
			if !changed {
				continue
			}
			encoded, err := json.Marshal(filtered)
			if err != nil {
				return fmt.Errorf("encode legacy group preset %d: %w", preset.ID, err)
			}
			if err := db.Model(&model.GroupPreset{}).Where("id = ?", preset.ID).Update("items", string(encoded)).Error; err != nil {
				return fmt.Errorf("clean legacy group preset %d: %w", preset.ID, err)
			}
		}
	}

	return nil
}

func isRemovedRouteMetadata(metadata *model.SiteModelRouteMetadata) bool {
	if metadata == nil {
		return false
	}
	if model.ContainsRemovedSiteModelRouteMarker(metadata.UnsupportedReason) {
		return true
	}
	for _, endpoint := range append(append([]string{}, metadata.SupportedEndpointTypes...), metadata.NormalizedEndpointTypes...) {
		if model.ContainsRemovedSiteModelRouteMarker(endpoint) {
			return true
		}
	}
	return false
}
