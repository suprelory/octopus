package model

import "testing"

func TestTurnstileSiteOptionsStayOnSiteAndCannotReplaceCredentials(t *testing.T) {
	for _, value := range []CheckinTurnstileSite{
		{PagePath: "https://external.example/login"}, {PagePath: "//external.example"},
		{PagePath: "/login?token=secret"}, {PagePath: "/login#secret"}, {PagePath: "/../login"},
		{PagePath: "/login?"}, {PagePath: "/{{checkin_date}}"},
		{SiteKey: "bad\nkey"}, {Action: "user@example.com"},
		{TokenLocation: "header", TokenField: "Authorization"},
		{TokenLocation: "header", TokenField: "Host"},
		{TokenLocation: "header", TokenField: "User-Agent"},
		{TokenLocation: "json", TokenField: "refresh_token"},
		{TokenLocation: "query", TokenField: "password"},
	} {
		if NormalizeCheckinTurnstileSite(&value) == nil {
			t.Fatalf("unsafe widget options accepted: %+v", value)
		}
	}
	for _, platform := range CheckinPlatforms() {
		config := CheckinSiteConfig{Name: "site", BaseURL: "https://example.test/prefix", Adapter: platform.ID, Submit: CheckinRequest{Path: platform.Path}, Turnstile: &CheckinTurnstileSite{SiteKey: "0x-key", PagePath: "/login", TokenLocation: "header", TokenField: "X-Turnstile-Token"}}
		if config.Submit.Path == "" {
			config.Submit.Path = "/checkin"
		}
		if err := NormalizeCheckinSite(&config); err != nil {
			t.Fatalf("%s disallowed Turnstile: %v", platform.ID, err)
		}
	}
	for _, body := range []string{`[]`, `null`, `"value"`} {
		config := CheckinSiteConfig{Name: "site", BaseURL: "https://example.test", Adapter: "http", Submit: CheckinRequest{Path: "/checkin", Body: body}, Turnstile: &CheckinTurnstileSite{TokenLocation: "json"}}
		if NormalizeCheckinSite(&config) == nil {
			t.Fatal("non-object body would consume a solver token without a valid retry")
		}
	}
}
