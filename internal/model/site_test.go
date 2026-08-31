package model

import (
	"strings"
	"testing"
)

func TestNormalizeComparableSiteTokenValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "strips sk prefix", input: "sk-abc123", expected: "abc123"},
		{name: "strips uppercase prefix", input: "SK-abc123", expected: "abc123"},
		{name: "keeps non prefixed token", input: "abc123", expected: "abc123"},
		{name: "trims whitespace", input: "  sk-abc123  ", expected: "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := NormalizeComparableSiteTokenValue(tt.input); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestSiteMaskedTokenMatchesRequiresHiddenCharacter(t *testing.T) {
	if SiteMaskedTokenMatches("abcdef", "abc***def") {
		t.Fatalf("expected token with no hidden characters to be rejected")
	}
	if !SiteMaskedTokenMatches("abcXdef", "abc***def") {
		t.Fatalf("expected token with at least one hidden character to match")
	}
}

func TestSiteMaskedTokenMatchesWithBulletMask(t *testing.T) {
	if !SiteMaskedTokenMatches("prefix-secret-suffix", "prefix••••••suffix") {
		t.Fatal("expected bullet-masked token to match without splitting multibyte mask runes")
	}
}

func TestNormalizeSiteTokenValueStatusRestoresReadyWhenTokenIsComplete(t *testing.T) {
	if actual := NormalizeSiteTokenValueStatus(SiteTokenValueStatusMaskedPending, "sk-real-token"); actual != SiteTokenValueStatusReady {
		t.Fatalf("expected full token to restore ready status, got %q", actual)
	}
	if actual := NormalizeSiteTokenValueStatus(SiteTokenValueStatusReady, "yzFy**********OTkb"); actual != SiteTokenValueStatusMaskedPending {
		t.Fatalf("expected masked token to stay masked_pending, got %q", actual)
	}
}

func TestNormalizeSiteSyncTokenValueForPlatform(t *testing.T) {
	tests := []struct {
		name     string
		platform SitePlatform
		input    string
		expected string
	}{
		{name: "new-api adds prefix", platform: SitePlatformNewAPI, input: "abc123", expected: "sk-abc123"},
		{name: "new-api keeps existing prefix", platform: SitePlatformNewAPI, input: "sk-abc123", expected: "sk-abc123"},
		{name: "one-hub adds prefix", platform: SitePlatformOneHub, input: "abc123", expected: "sk-abc123"},
		{name: "api keeps verbatim", platform: SitePlatformAPI, input: "abc123", expected: "abc123"},
		{name: "api keeps verbatim 2", platform: SitePlatformAPI, input: "AIzaXXXX", expected: "AIzaXXXX"},
		{name: "api keeps verbatim and trims", platform: SitePlatformAPI, input: "  sk-ant-abc  ", expected: "sk-ant-abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := NormalizeSiteSyncTokenValueForPlatform(tt.platform, tt.input); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestResolveRouteBaseURL(t *testing.T) {
	site := &Site{
		BaseURL: "https://example.com",
		RouteBaseURLs: []SiteRouteBaseURL{
			{RouteType: SiteModelRouteTypeAnthropic, BaseURL: "https://example.com/anthropic/v1/"},
			{RouteType: SiteModelRouteTypeGemini, BaseURL: "   "},
		},
	}

	if got, ok := site.ResolveRouteBaseURL(SiteModelRouteTypeAnthropic); !ok || got != "https://example.com/anthropic/v1" {
		t.Fatalf("expected anthropic override trimmed, got %q ok=%v", got, ok)
	}
	if _, ok := site.ResolveRouteBaseURL(SiteModelRouteTypeGemini); ok {
		t.Fatalf("expected blank override to be treated as absent")
	}
	if _, ok := site.ResolveRouteBaseURL(SiteModelRouteTypeOpenAIResponse); ok {
		t.Fatalf("expected missing route type to have no override")
	}
	var nilSite *Site
	if _, ok := nilSite.ResolveRouteBaseURL(SiteModelRouteTypeAnthropic); ok {
		t.Fatalf("expected nil site to have no override")
	}
}

func TestNormalizeSiteRouteBaseURLs(t *testing.T) {
	items := []SiteRouteBaseURL{
		{RouteType: SiteModelRouteTypeAnthropic, BaseURL: "  https://example.com/anthropic/v1/  "},
		{RouteType: SiteModelRouteTypeAnthropic, BaseURL: "https://example.com/dup"},
		{RouteType: "  ", BaseURL: "https://example.com/x"},
		{RouteType: SiteModelRouteTypeGemini, BaseURL: ""},
	}
	got := NormalizeSiteRouteBaseURLs(items)
	if len(got) != 1 {
		t.Fatalf("expected one normalized entry, got %d (%+v)", len(got), got)
	}
	if got[0].RouteType != SiteModelRouteTypeAnthropic || got[0].BaseURL != "https://example.com/anthropic/v1" {
		t.Fatalf("unexpected normalized entry: %+v", got[0])
	}
}

func TestNormalizeSiteRouteBaseURLsDropsRemovedVolcengineOverrides(t *testing.T) {
	items := []SiteRouteBaseURL{
		{RouteType: SiteModelRouteType("volcengine"), BaseURL: "https://example.com/ark"},
		{RouteType: SiteModelRouteType("ark"), BaseURL: "https://example.com/ark-v2"},
		{RouteType: SiteModelRouteTypeOpenAIChat, BaseURL: "https://example.com/v1"},
	}
	got := NormalizeSiteRouteBaseURLs(items)
	if len(got) != 1 || got[0].RouteType != SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected only supported base URL override to remain, got %+v", got)
	}
}

func TestResolveRouteBaseURLDoesNotReuseRemovedOverrideForUnknownRoute(t *testing.T) {
	site := &Site{RouteBaseURLs: []SiteRouteBaseURL{
		{RouteType: SiteModelRouteType("volcengine"), BaseURL: "https://example.com/ark"},
	}}
	if _, ok := site.ResolveRouteBaseURL(SiteModelRouteTypeUnknown); ok {
		t.Fatal("expected removed route override not to resolve for unknown route")
	}
}

func TestRemovedRouteTypesNormalizeToUnknownAndDoNotComposeBindingSuffix(t *testing.T) {
	for _, routeType := range []SiteModelRouteType{"volcengine", "ARK"} {
		if got := NormalizeSiteModelRouteType(routeType); got != SiteModelRouteTypeUnknown {
			t.Fatalf("expected %q to normalize to unknown, got %q", routeType, got)
		}
		base, parsed := ParseSiteChannelBindingKey("group::" + strings.ToLower(string(routeType)))
		if base != "group" || parsed != SiteModelRouteTypeUnknown {
			t.Fatalf("expected removed binding suffix to parse as unknown, got base=%q route=%q", base, parsed)
		}
	}
	if got := ComposeSiteChannelBindingKey("group", SiteModelRouteTypeUnknown, true); got != "group" {
		t.Fatalf("expected unknown route to avoid a projected suffix, got %q", got)
	}
}

func TestValidateSiteRouteBaseURLs(t *testing.T) {
	tests := []struct {
		name    string
		items   []SiteRouteBaseURL
		wantErr bool
	}{
		{
			name:    "valid http override",
			items:   []SiteRouteBaseURL{{RouteType: SiteModelRouteTypeAnthropic, BaseURL: "https://example.com/anthropic/v1"}},
			wantErr: false,
		},
		{
			name:    "unsupported route type",
			items:   []SiteRouteBaseURL{{RouteType: SiteModelRouteTypeUnknown, BaseURL: "https://example.com/v1"}},
			wantErr: true,
		},
		{
			name:    "missing scheme",
			items:   []SiteRouteBaseURL{{RouteType: SiteModelRouteTypeAnthropic, BaseURL: "example.com/anthropic/v1"}},
			wantErr: true,
		},
		{
			name:    "missing host",
			items:   []SiteRouteBaseURL{{RouteType: SiteModelRouteTypeAnthropic, BaseURL: "https:///anthropic"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSiteRouteBaseURLs(tt.items)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSiteRouteBaseURLs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSiteValidateNormalizesCheckinScheduleDefaults(t *testing.T) {
	site := &Site{
		Name:     "schedule-defaults",
		Platform: SitePlatformNewAPI,
		BaseURL:  "https://example.com",
	}
	if err := site.Validate(); err != nil {
		t.Fatalf("Site.Validate failed: %v", err)
	}
	if site.CheckinTimezone != DefaultSiteCheckinTimezone ||
		site.CheckinWindowStart != DefaultSiteCheckinWindowStart ||
		site.CheckinWindowEnd != DefaultSiteCheckinWindowEnd {
		t.Fatalf("unexpected checkin defaults: timezone=%q start=%q end=%q", site.CheckinTimezone, site.CheckinWindowStart, site.CheckinWindowEnd)
	}
}

func TestSiteValidateRejectsInvalidCheckinSchedule(t *testing.T) {
	tests := []struct {
		name     string
		timezone string
		start    string
		end      string
	}{
		{name: "invalid timezone", timezone: "Not/A_Timezone", start: "08:00", end: "23:00"},
		{name: "invalid start", timezone: "Asia/Shanghai", start: "8am", end: "23:00"},
		{name: "end before start", timezone: "Asia/Shanghai", start: "20:00", end: "08:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := &Site{
				Name:               "invalid-schedule",
				Platform:           SitePlatformNewAPI,
				BaseURL:            "https://example.com",
				CheckinTimezone:    tt.timezone,
				CheckinWindowStart: tt.start,
				CheckinWindowEnd:   tt.end,
			}
			if err := site.Validate(); err == nil {
				t.Fatalf("expected invalid checkin schedule to fail validation")
			}
		})
	}
}

func TestCompactSiteModelRouteTypeName(t *testing.T) {
	tests := []struct {
		name      string
		routeType SiteModelRouteType
		expected  string
	}{
		{name: "chat", routeType: SiteModelRouteTypeOpenAIChat, expected: "Chat"},
		{name: "response", routeType: SiteModelRouteTypeOpenAIResponse, expected: "Response"},
		{name: "anthropic", routeType: SiteModelRouteTypeAnthropic, expected: "Anthropic"},
		{name: "gemini", routeType: SiteModelRouteTypeGemini, expected: "Gemini"},
		{name: "embedding", routeType: SiteModelRouteTypeOpenAIEmbedding, expected: "Embedding"},
		{name: "unknown", routeType: SiteModelRouteTypeUnknown, expected: "Unsupported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := CompactSiteModelRouteTypeName(tt.routeType); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestInferSiteModelRouteType(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		expected  SiteModelRouteType
	}{
		{name: "anthropic models stay anthropic", modelName: "claude-3-5-sonnet", expected: SiteModelRouteTypeAnthropic},
		{name: "gemini models stay gemini", modelName: "gemini-2.0-flash", expected: SiteModelRouteTypeGemini},
		{name: "embedding models use embedding route", modelName: "text-embedding-3-large", expected: SiteModelRouteTypeOpenAIEmbedding},
		{name: "gpt 4o defaults to chat without metadata", modelName: "gpt-4o-mini", expected: SiteModelRouteTypeOpenAIChat},
		{name: "gpt 4.1 defaults to chat without metadata", modelName: "gpt-4.1", expected: SiteModelRouteTypeOpenAIChat},
		{name: "gpt 5 defaults to chat without metadata", modelName: "gpt-5-mini", expected: SiteModelRouteTypeOpenAIChat},
		{name: "o series defaults to chat without metadata", modelName: "o3-mini", expected: SiteModelRouteTypeOpenAIChat},
		{name: "older openai chat models remain chat", modelName: "gpt-4-turbo", expected: SiteModelRouteTypeOpenAIChat},
		{name: "generic compat models remain chat", modelName: "deepseek-chat", expected: SiteModelRouteTypeOpenAIChat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := InferSiteModelRouteType(tt.modelName); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}
