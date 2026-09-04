package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DBDump is a full-database JSON export format for Octopus.
// Import uses incremental semantics (insert new rows, and upsert on certain key-based tables).
type DBDump struct {
	Version      int       `json:"version"`
	ExportedAt   time.Time `json:"exported_at"`
	IncludeLogs  bool      `json:"include_logs"`
	IncludeStats bool      `json:"include_stats"`

	Channels            []Channel            `json:"channels,omitempty"`
	ChannelKeys         []ChannelKey         `json:"channel_keys,omitempty"`
	ProxyConfigurations []ProxyConfiguration `json:"proxy_configurations,omitempty"`
	Sites               []Site               `json:"sites,omitempty"`
	SiteAccounts        []SiteAccount        `json:"site_accounts,omitempty"`
	SiteTokens          []SiteToken          `json:"site_tokens,omitempty"`
	SiteUserGroups      []SiteUserGroup      `json:"site_user_groups,omitempty"`
	SiteModels          []SiteModel          `json:"site_models,omitempty"`
	SiteChannelBindings []SiteChannelBinding `json:"site_channel_bindings,omitempty"`
	Groups              []Group              `json:"groups,omitempty"`
	GroupItems          []GroupItem          `json:"group_items,omitempty"`
	LLMInfos            []LLMInfo            `json:"llm_infos,omitempty"`
	APIKeys             []APIKey             `json:"api_keys,omitempty"`
	Settings            []Setting            `json:"settings,omitempty"`

	StatsTotal           []StatsTotal           `json:"stats_total,omitempty"`
	StatsDaily           []StatsDaily           `json:"stats_daily,omitempty"`
	StatsHourly          []StatsHourly          `json:"stats_hourly,omitempty"`
	StatsModel           []StatsModel           `json:"stats_model,omitempty"`
	StatsChannel         []StatsChannel         `json:"stats_channel,omitempty"`
	StatsAPIKey          []StatsAPIKey          `json:"stats_api_key,omitempty"`
	StatsSiteModelHourly []StatsSiteModelHourly `json:"stats_site_model_hourly,omitempty"`

	RelayLogs []RelayLog `json:"relay_logs,omitempty"`
}

type legacyDumpProxyFields struct {
	Channels []struct {
		Proxy        bool    `json:"proxy"`
		ChannelProxy *string `json:"channel_proxy"`
	} `json:"channels"`
	Sites []struct {
		Proxy          bool    `json:"proxy"`
		SiteProxy      *string `json:"site_proxy"`
		UseSystemProxy bool    `json:"use_system_proxy"`
	} `json:"sites"`
	SiteAccounts []struct {
		AccountProxy *string `json:"account_proxy"`
	} `json:"site_accounts"`
}

const legacyDumpSiteCheckinIntervalSetting SettingKey = "site_checkin_interval"

// UnmarshalJSON upgrades proxy fields from pre-pool backups while keeping the
// legacy representation out of the runtime channel, site, and account models.
func (d *DBDump) UnmarshalJSON(data []byte) error {
	type dumpAlias DBDump
	var decoded dumpAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*d = DBDump(decoded)

	var legacy legacyDumpProxyFields
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	d.upgradeLegacyDumpProxyFields(legacy)
	d.upgradeLegacyDumpSettings()
	return nil
}

func (d *DBDump) upgradeLegacyDumpSettings() {
	canonical := d.Settings[:0]
	for _, setting := range d.Settings {
		if setting.Key == legacyDumpSiteCheckinIntervalSetting {
			continue
		}
		if setting.Key == SettingKeyProjectedChannelAutoGroupEnabled {
			switch strings.ToLower(strings.TrimSpace(setting.Value)) {
			case "true":
				setting.Value = "1"
			case "", "false":
				setting.Value = "0"
			}
		}
		canonical = append(canonical, setting)
	}
	d.Settings = canonical
}

func (d *DBDump) upgradeLegacyDumpProxyFields(legacy legacyDumpProxyFields) {
	proxyIDByURL := make(map[string]int, len(d.ProxyConfigurations))
	usedIDs := make(map[int]struct{}, len(d.ProxyConfigurations))
	for _, proxyConfig := range d.ProxyConfigurations {
		usedIDs[proxyConfig.ID] = struct{}{}
		if normalized, err := NormalizeProxyURL(proxyConfig.URL); err == nil && proxyConfig.ID != 0 {
			proxyIDByURL[normalized] = proxyConfig.ID
		}
	}
	nextLegacyID := -1
	ensureProxyConfig := func(raw *string) *int {
		if raw == nil {
			return nil
		}
		normalized, err := NormalizeProxyURL(*raw)
		if err != nil {
			return nil
		}
		if id, ok := proxyIDByURL[normalized]; ok {
			return &id
		}
		for {
			if _, exists := usedIDs[nextLegacyID]; !exists {
				break
			}
			nextLegacyID--
		}
		id := nextLegacyID
		nextLegacyID--
		usedIDs[id] = struct{}{}
		proxyIDByURL[normalized] = id
		d.ProxyConfigurations = append(d.ProxyConfigurations, ProxyConfiguration{
			ID:      id,
			Name:    fmt.Sprintf("Imported Proxy %d", len(proxyIDByURL)),
			URL:     normalized,
			Enabled: true,
			Remark:  "由历史备份代理配置迁移生成",
		})
		return &id
	}

	for i := 0; i < len(d.Channels) && i < len(legacy.Channels); i++ {
		channel := &d.Channels[i]
		if strings.TrimSpace(string(channel.ProxyMode)) != "" {
			continue
		}
		legacyChannel := legacy.Channels[i]
		if !legacyChannel.Proxy {
			channel.ProxyMode = ProxyUsageModeDirect
			channel.ProxyConfigID = nil
		} else if proxyConfigID := ensureProxyConfig(legacyChannel.ChannelProxy); proxyConfigID != nil {
			channel.ProxyMode = ProxyUsageModePool
			channel.ProxyConfigID = proxyConfigID
		} else {
			channel.ProxyMode = ProxyUsageModeSystem
			channel.ProxyConfigID = nil
		}
	}
	for i := 0; i < len(d.Sites) && i < len(legacy.Sites); i++ {
		site := &d.Sites[i]
		if strings.TrimSpace(string(site.ProxyMode)) != "" {
			continue
		}
		legacySite := legacy.Sites[i]
		if legacySite.Proxy {
			if proxyConfigID := ensureProxyConfig(legacySite.SiteProxy); proxyConfigID != nil {
				site.ProxyMode = ProxyUsageModePool
				site.ProxyConfigID = proxyConfigID
			} else {
				site.ProxyMode = ProxyUsageModeSystem
				site.ProxyConfigID = nil
			}
		} else if legacySite.UseSystemProxy {
			site.ProxyMode = ProxyUsageModeSystem
			site.ProxyConfigID = nil
		} else {
			site.ProxyMode = ProxyUsageModeDirect
			site.ProxyConfigID = nil
		}
	}
	for i := 0; i < len(d.SiteAccounts) && i < len(legacy.SiteAccounts); i++ {
		account := &d.SiteAccounts[i]
		if strings.TrimSpace(string(account.ProxyMode)) != "" {
			continue
		}
		if proxyConfigID := ensureProxyConfig(legacy.SiteAccounts[i].AccountProxy); proxyConfigID != nil {
			account.ProxyMode = ProxyUsageModePool
			account.ProxyConfigID = proxyConfigID
		} else {
			account.ProxyMode = ProxyUsageModeInherit
			account.ProxyConfigID = nil
		}
	}
}

type DBImportResult struct {
	// RowsAffected contains the rows affected for each table operation (insert/upsert depending on table).
	RowsAffected map[string]int64 `json:"rows_affected"`
}
