package model

import "testing"

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
