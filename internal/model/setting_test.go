package model

import "testing"

func TestTrustedProxiesSetting(t *testing.T) {
	defaults := make(map[SettingKey]string)
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}
	if got := defaults[SettingKeyTrustedProxies]; got != "" {
		t.Fatalf("trusted proxies default = %q, want empty", got)
	}

	setting := Setting{Key: SettingKeyTrustedProxies, Value: "172.24.0.1\n10.0.0.0/24,172.24.0.1"}
	if err := setting.Validate(); err != nil {
		t.Fatalf("valid trusted proxies rejected: %v", err)
	}
	if setting.Value != "10.0.0.0/24,172.24.0.1" {
		t.Fatalf("normalized trusted proxies = %q", setting.Value)
	}

	invalid := Setting{Key: SettingKeyTrustedProxies, Value: "proxy.example.com"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected hostname trusted proxy to be rejected")
	}
}

func TestChannelAffinityDefaultSettings(t *testing.T) {
	defaults := make(map[SettingKey]string)
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}

	if got := defaults[SettingKeyChannelAffinityEnabled]; got != "true" {
		t.Fatalf("channel affinity enabled default = %q, want true", got)
	}
	if got := defaults[SettingKeyChannelAffinityTTLSeconds]; got != "3600" {
		t.Fatalf("channel affinity TTL default = %q, want 3600", got)
	}
}

func TestEmptyResponseDetectionSetting(t *testing.T) {
	defaults := make(map[SettingKey]string)
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}
	if got := defaults[SettingKeyEmptyResponseDetectionEnabled]; got != "true" {
		t.Fatalf("empty response detection default = %q, want true", got)
	}

	for _, value := range []string{"true", "false"} {
		setting := Setting{Key: SettingKeyEmptyResponseDetectionEnabled, Value: value}
		if err := setting.Validate(); err != nil {
			t.Fatalf("expected %q to be valid, got %v", value, err)
		}
	}
	invalid := Setting{Key: SettingKeyEmptyResponseDetectionEnabled, Value: "1"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected non-boolean value to be rejected")
	}
}

func TestCapabilityDegradationPolicySetting(t *testing.T) {
	defaults := make(map[SettingKey]string)
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}
	if got := defaults[SettingKeyCapabilityDegradationPolicy]; got != "warn" {
		t.Fatalf("capability degradation policy default = %q, want warn", got)
	}
	for _, value := range []string{"allow", "warn", "strict"} {
		setting := Setting{Key: SettingKeyCapabilityDegradationPolicy, Value: value}
		if err := setting.Validate(); err != nil {
			t.Fatalf("expected %q to be valid, got %v", value, err)
		}
	}
	invalid := Setting{Key: SettingKeyCapabilityDegradationPolicy, Value: "reject"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected unknown capability degradation policy to be rejected")
	}
}

func TestChannelAffinitySettingValidation(t *testing.T) {
	tests := []struct {
		name    string
		setting Setting
		valid   bool
	}{
		{name: "enabled true", setting: Setting{Key: SettingKeyChannelAffinityEnabled, Value: "true"}, valid: true},
		{name: "enabled false", setting: Setting{Key: SettingKeyChannelAffinityEnabled, Value: "false"}, valid: true},
		{name: "enabled invalid", setting: Setting{Key: SettingKeyChannelAffinityEnabled, Value: "1"}, valid: false},
		{name: "ttl minimum", setting: Setting{Key: SettingKeyChannelAffinityTTLSeconds, Value: "1"}, valid: true},
		{name: "ttl default", setting: Setting{Key: SettingKeyChannelAffinityTTLSeconds, Value: "3600"}, valid: true},
		{name: "ttl zero", setting: Setting{Key: SettingKeyChannelAffinityTTLSeconds, Value: "0"}, valid: false},
		{name: "ttl negative", setting: Setting{Key: SettingKeyChannelAffinityTTLSeconds, Value: "-1"}, valid: false},
		{name: "ttl non-integer", setting: Setting{Key: SettingKeyChannelAffinityTTLSeconds, Value: "one hour"}, valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.setting.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected setting to be valid, got %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected setting to be rejected")
			}
		})
	}
}
