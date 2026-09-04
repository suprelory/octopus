package model

import (
	"testing"
	"time"
)

func TestParseAutoGroupSettingValueAcceptsOnlyNumericModes(t *testing.T) {
	for _, test := range []struct {
		value string
		want  AutoGroupType
		valid bool
	}{
		{value: "0", want: AutoGroupTypeNone, valid: true},
		{value: " 1 ", want: AutoGroupTypeFuzzy, valid: true},
		{value: "2", want: AutoGroupTypeExact, valid: true},
		{value: "3", want: AutoGroupTypeRegex, valid: true},
		{value: "true", valid: false},
		{value: "false", valid: false},
		{value: "", valid: false},
		{value: "4", valid: false},
	} {
		got, valid := ParseAutoGroupSettingValue(test.value)
		if valid != test.valid || valid && got != test.want {
			t.Errorf("ParseAutoGroupSettingValue(%q) = (%d, %t), want (%d, %t)", test.value, got, valid, test.want, test.valid)
		}
	}
}

func TestGetChannelKeyPrefersPreferredKeyID(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "first", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesPreferredKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "fallback", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2 despite recent 429, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesLowestCostKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "recent-429", TotalCost: 1, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
			{ID: 2, Enabled: true, ChannelKey: "other", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey()
	if selected.ID != 1 {
		t.Fatalf("expected lowest cost key 1 despite recent 429, got %d", selected.ID)
	}
}
