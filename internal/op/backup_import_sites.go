package op

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

func (s *dbImportState) importSites() error {
	tx := s.tx
	dump := s.dump
	res := s.result
	proxyConfigIDMap := s.proxyIDs
	siteIDMap := s.siteIDs
	// 4. Sites (dedup by platform+base_url)
	for i := range dump.Sites {
		site := dump.Sites[i]
		oldID := site.ID
		site.ID = 0
		site.Accounts = nil
		remapProxyConfigID(&site.ProxyMode, &site.ProxyConfigID, proxyConfigIDMap)
		if model.IsRemovedSiteModelRouteType(site.DefaultRouteType) {
			site.DefaultRouteType = model.SiteModelRouteTypeOpenAIChat
		}
		site.RouteBaseURLs = model.NormalizeSiteRouteBaseURLs(site.RouteBaseURLs)

		// Preserve the path in base_url (e.g. https://opencode.ai/zen/v1):
		// native backups already hold full, canonical URLs. Only trim like
		// Site.Normalize so dedup compares against the stored value. (Do not
		// use normalizeImportBaseURL here — it strips the path, which is only
		// correct for third-party imports.)
		site.BaseURL = strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")

		var existing model.Site
		if err := tx.Where("platform = ? AND base_url = ?", site.Platform, site.BaseURL).First(&existing).Error; err == nil {
			siteIDMap[oldID] = existing.ID
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import sites: %w", err)
		}
		site.Name = uniqueSiteName(tx, site.Name)
		if err := tx.Omit("Accounts").Create(&site).Error; err != nil {
			return fmt.Errorf("import sites: %w", err)
		}
		siteIDMap[oldID] = site.ID
		res.RowsAffected["sites"]++
	}
	return nil
}

func (s *dbImportState) importAccounts() error {
	tx := s.tx
	dump := s.dump
	res := s.result
	proxyConfigIDMap := s.proxyIDs
	siteIDMap := s.siteIDs
	accountIDMap := s.accountIDs
	// 5. SiteAccounts (remap site_id, dedup by site_id+name)
	for i := range dump.SiteAccounts {
		account := dump.SiteAccounts[i]
		oldID := account.ID
		account.ID = 0
		account.Tokens = nil
		account.UserGroups = nil
		account.Models = nil
		account.ChannelBindings = nil
		remapProxyConfigID(&account.ProxyMode, &account.ProxyConfigID, proxyConfigIDMap)

		if newSiteID, ok := siteIDMap[account.SiteID]; ok {
			account.SiteID = newSiteID
		}

		var existing model.SiteAccount
		if err := tx.Where("site_id = ? AND name = ?", account.SiteID, strings.TrimSpace(account.Name)).First(&existing).Error; err == nil {
			accountIDMap[oldID] = existing.ID
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import site_accounts: %w", err)
		}
		if err := tx.Omit("Tokens", "UserGroups", "Models", "ChannelBindings").Create(&account).Error; err != nil {
			return fmt.Errorf("import site_accounts: %w", err)
		}
		accountIDMap[oldID] = account.ID
		res.RowsAffected["site_accounts"]++
	}
	return nil
}

func (s *dbImportState) importTokens() error {
	tx := s.tx
	dump := s.dump
	res := s.result
	accountIDMap := s.accountIDs
	// 6. SiteTokens (remap site_account_id, dedup by site_account_id+token+group_key)
	for i := range dump.SiteTokens {
		token := dump.SiteTokens[i]
		token.ID = 0
		if newID, ok := accountIDMap[token.SiteAccountID]; ok {
			token.SiteAccountID = newID
		}
		var existing model.SiteToken
		if err := tx.Where("site_account_id = ? AND token = ? AND group_key = ?", token.SiteAccountID, token.Token, token.GroupKey).First(&existing).Error; err == nil {
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import site_tokens: %w", err)
		}
		if err := tx.Create(&token).Error; err != nil {
			return fmt.Errorf("import site_tokens: %w", err)
		}
		res.RowsAffected["site_tokens"]++
	}
	return nil
}

func (s *dbImportState) importUserGroups() error {
	tx := s.tx
	dump := s.dump
	res := s.result
	accountIDMap := s.accountIDs
	userGroupIDMap := s.userGroupIDs
	// 7. SiteUserGroups (remap site_account_id, dedup by uniqueIndex)
	for i := range dump.SiteUserGroups {
		group := dump.SiteUserGroups[i]
		oldID := group.ID
		group.ID = 0
		if newID, ok := accountIDMap[group.SiteAccountID]; ok {
			group.SiteAccountID = newID
		}
		var existing model.SiteUserGroup
		if err := tx.Where("site_account_id = ? AND group_key = ?", group.SiteAccountID, group.GroupKey).First(&existing).Error; err == nil {
			userGroupIDMap[oldID] = existing.ID
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import site_user_groups: %w", err)
		}
		if err := tx.Create(&group).Error; err != nil {
			return fmt.Errorf("import site_user_groups: %w", err)
		}
		userGroupIDMap[oldID] = group.ID
		res.RowsAffected["site_user_groups"]++
	}
	return nil
}

func (s *dbImportState) importModels() error {
	tx := s.tx
	dump := s.dump
	res := s.result
	accountIDMap := s.accountIDs
	// 8. SiteModels (remap site_account_id, dedup by uniqueIndex)
	for i := range dump.SiteModels {
		m := dump.SiteModels[i]
		normalizeImportedSiteModelRoute(&m)
		m.ID = 0
		if newID, ok := accountIDMap[m.SiteAccountID]; ok {
			m.SiteAccountID = newID
		}
		var existing model.SiteModel
		if err := tx.Where("site_account_id = ? AND group_key = ? AND model_name = ?", m.SiteAccountID, m.GroupKey, m.ModelName).First(&existing).Error; err == nil {
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import site_models: %w", err)
		}
		if err := tx.Create(&m).Error; err != nil {
			return fmt.Errorf("import site_models: %w", err)
		}
		res.RowsAffected["site_models"]++
	}
	return nil
}

func (s *dbImportState) importBindings() error {
	tx := s.tx
	dump := s.dump
	res := s.result
	siteIDMap := s.siteIDs
	accountIDMap := s.accountIDs
	userGroupIDMap := s.userGroupIDs
	resolveImportedChannel := s.resolveChannel
	// 9. SiteChannelBindings (remap all FKs, dedup by both unique constraints)
	for i := range dump.SiteChannelBindings {
		binding := dump.SiteChannelBindings[i]
		resolved, err := resolveImportedChannel(binding.ChannelID)
		if err != nil {
			return fmt.Errorf("import site_channel_bindings: resolve channel: %w", err)
		}
		if !resolved.Supported {
			log.Warnw("skip missing or unsupported site channel binding during import", "channel_id", binding.ChannelID)
			continue
		}
		binding.ID = 0
		if newID, ok := siteIDMap[binding.SiteID]; ok {
			binding.SiteID = newID
		}
		if newID, ok := accountIDMap[binding.SiteAccountID]; ok {
			binding.SiteAccountID = newID
		}
		if binding.SiteUserGroupID != nil {
			if newID, ok := userGroupIDMap[*binding.SiteUserGroupID]; ok {
				binding.SiteUserGroupID = &newID
			}
		}
		binding.ChannelID = resolved.ID

		var existing model.SiteChannelBinding
		if err := tx.Where("site_account_id = ? AND group_key = ?", binding.SiteAccountID, binding.GroupKey).First(&existing).Error; err == nil {
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import site_channel_bindings: %w", err)
		}
		if err := tx.Where("channel_id = ?", binding.ChannelID).First(&existing).Error; err == nil {
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("import site_channel_bindings: %w", err)
		}
		if err := tx.Create(&binding).Error; err != nil {
			return fmt.Errorf("import site_channel_bindings: %w", err)
		}
		res.RowsAffected["site_channel_bindings"]++
	}
	return nil
}

func normalizeImportedSiteModelRoute(item *model.SiteModel) {
	if item == nil {
		return
	}

	rawRouteType := item.RouteType
	normalizedRouteType := model.NormalizeSiteModelRouteType(rawRouteType)
	legacyRoute := model.IsRemovedSiteModelRouteType(rawRouteType)
	legacyMetadata := model.ContainsRemovedSiteModelRouteMarker(item.RouteRawPayload)
	manualSupportedRoute := item.ManualOverride &&
		!legacyRoute &&
		!legacyMetadata &&
		model.IsProjectedSiteModelRouteType(normalizedRouteType)
	legacyModel := model.ContainsRemovedSiteModelRouteMarker(item.ModelName) && !manualSupportedRoute

	if !legacyRoute && !legacyMetadata && !legacyModel {
		item.RouteType = normalizedRouteType
		item.RouteSource = model.NormalizeSiteModelRouteSource(item.RouteSource, item.ManualOverride)
		return
	}

	metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
	if !ok {
		metadata = &model.SiteModelRouteMetadata{}
	}
	metadata.RouteSupported = false
	metadata.RouteGuessed = false
	metadata.RouteType = model.SiteModelRouteTypeUnknown
	if strings.TrimSpace(metadata.UnsupportedReason) == "" {
		metadata.UnsupportedReason = "Volcengine/Ark route support has been removed"
	}
	item.RouteType = model.SiteModelRouteTypeUnknown
	item.RouteSource = model.SiteModelRouteSourceSyncInferred
	item.ManualOverride = false
	item.RouteRawPayload = metadata.Marshal()
}
