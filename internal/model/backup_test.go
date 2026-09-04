package model

import (
	"encoding/json"
	"testing"
)

func TestDBDumpUnmarshalUpgradesLegacyProxyFields(t *testing.T) {
	data := []byte(`{
		"version":1,
		"channels":[
			{"id":1,"name":"pooled","proxy":true,"channel_proxy":"HTTP://Proxy.Example:8080"},
			{"id":2,"name":"system","proxy":true}
		],
			"sites":[
				{"id":3,"name":"pooled-site","proxy":true,"site_proxy":"http://proxy.example:8080"},
				{"id":4,"name":"system-site","use_system_proxy":true}
			],
			"site_accounts":[{"id":5,"account_proxy":"http://proxy.example:8080"}],
			"settings":[
				{"key":"projected_channel_auto_group_enabled","value":" true "},
				{"key":"site_checkin_interval","value":"24"},
				{"key":"proxy_url","value":""}
			]
		}`)

	var dump DBDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatalf("unmarshal legacy dump: %v", err)
	}
	if len(dump.ProxyConfigurations) != 1 {
		t.Fatalf("proxy configuration count = %d, want 1", len(dump.ProxyConfigurations))
	}
	proxyConfigID := dump.ProxyConfigurations[0].ID
	if proxyConfigID >= 0 || dump.ProxyConfigurations[0].URL != "http://proxy.example:8080" {
		t.Fatalf("unexpected migrated proxy configuration: %+v", dump.ProxyConfigurations[0])
	}
	if dump.Channels[0].ProxyMode != ProxyUsageModePool || dump.Channels[0].ProxyConfigID == nil || *dump.Channels[0].ProxyConfigID != proxyConfigID {
		t.Fatalf("unexpected pooled channel proxy: %+v", dump.Channels[0])
	}
	if dump.Channels[1].ProxyMode != ProxyUsageModeSystem || dump.Channels[1].ProxyConfigID != nil {
		t.Fatalf("unexpected system channel proxy: %+v", dump.Channels[1])
	}
	if dump.Sites[0].ProxyMode != ProxyUsageModePool || dump.Sites[0].ProxyConfigID == nil || *dump.Sites[0].ProxyConfigID != proxyConfigID {
		t.Fatalf("unexpected pooled site proxy: %+v", dump.Sites[0])
	}
	if dump.Sites[1].ProxyMode != ProxyUsageModeSystem || dump.Sites[1].ProxyConfigID != nil {
		t.Fatalf("unexpected system site proxy: %+v", dump.Sites[1])
	}
	if dump.SiteAccounts[0].ProxyMode != ProxyUsageModePool || dump.SiteAccounts[0].ProxyConfigID == nil || *dump.SiteAccounts[0].ProxyConfigID != proxyConfigID {
		t.Fatalf("unexpected account proxy: %+v", dump.SiteAccounts[0])
	}
	if len(dump.Settings) != 2 {
		t.Fatalf("setting count = %d, want 2: %+v", len(dump.Settings), dump.Settings)
	}
	if dump.Settings[0].Key != SettingKeyProjectedChannelAutoGroupEnabled || dump.Settings[0].Value != "1" {
		t.Fatalf("unexpected migrated auto-group setting: %+v", dump.Settings[0])
	}
	if dump.Settings[1].Key != SettingKeyProxyURL {
		t.Fatalf("unexpected retained setting: %+v", dump.Settings[1])
	}
}

func TestDBDumpUnmarshalPrefersCanonicalProxyFields(t *testing.T) {
	data := []byte(`{
		"version":1,
		"channels":[{
			"id":1,
			"name":"canonical",
			"proxy_mode":"direct",
			"proxy":true,
			"channel_proxy":"http://legacy.example:8080"
		}]
	}`)

	var dump DBDump
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatalf("unmarshal canonical dump: %v", err)
	}
	if dump.Channels[0].ProxyMode != ProxyUsageModeDirect || dump.Channels[0].ProxyConfigID != nil {
		t.Fatalf("legacy fields overrode canonical proxy settings: %+v", dump.Channels[0])
	}
	if len(dump.ProxyConfigurations) != 0 {
		t.Fatalf("legacy proxy configuration was created for canonical data: %+v", dump.ProxyConfigurations)
	}
}
