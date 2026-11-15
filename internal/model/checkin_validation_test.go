package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckinConfigDoesNotPersistCredentialMaterialInSiteFields(t *testing.T) {
	tests := []struct {
		name   string
		config CheckinSiteConfig
	}{
		{
			name:   "authenticated proxy",
			config: CheckinSiteConfig{Name: "site", BaseURL: "https://example.com", Adapter: "http", ProxyURL: "http://proxy-user:proxy-pass@127.0.0.1:8080", Submit: CheckinRequest{Method: "POST", Path: "/checkin"}},
		},
		{
			name:   "query credential in manual URL",
			config: CheckinSiteConfig{Name: "site", BaseURL: "https://example.com", Adapter: "http", ManualURL: "https://example.com/checkin?token=private", Submit: CheckinRequest{Method: "POST", Path: "/checkin"}},
		},
		{
			name:   "secret auth header",
			config: CheckinSiteConfig{Name: "site", BaseURL: "https://example.com", Adapter: "http", Submit: CheckinRequest{Method: "POST", Path: "/checkin", Headers: []CheckinHeader{{Name: "X-Api-Key", Value: "private"}}}},
		},
		{
			name:   "credential in nested JSON body",
			config: CheckinSiteConfig{Name: "site", BaseURL: "https://example.com", Adapter: "http", Submit: CheckinRequest{Method: "POST", Path: "/checkin", Body: `{"data":{"access-token":"private"}}`}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := NormalizeCheckinSite(&test.config); err == nil {
				t.Fatal("credential-bearing site configuration was accepted")
			} else if strings.Contains(err.Error(), "private") {
				t.Fatal("validation echoed secret material")
			}
		})
	}

	config := CheckinSiteConfig{Name: "site", BaseURL: "https://example.com", Adapter: "http", Submit: CheckinRequest{Method: "POST", Path: "/checkin", Body: `{"date":"{{checkin_date}}","user_id":"{{user_id}}"}`}}
	if err := NormalizeCheckinSite(&config); err != nil {
		t.Fatalf("safe request placeholders were rejected: %v", err)
	}
	var persisted map[string]any
	encoded, err := json.Marshal(config)
	if err != nil || json.Unmarshal(encoded, &persisted) != nil {
		t.Fatal("normalized site configuration could not be serialized", err)
	}
}

func TestSub2APIRequiresAnExplicitEndpointAndSharedUserAgent(t *testing.T) {
	config := CheckinSiteConfig{Name: "Sub2API", BaseURL: "https://example.invalid", Adapter: "sub2api"}
	if NormalizeCheckinSite(&config) == nil {
		t.Fatal("Sub2API invented a built-in check-in endpoint")
	}
	config.Submit.Path = "/custom-checkin"
	if err := NormalizeCheckinSite(&config); err != nil {
		t.Fatal(err)
	}
	if config.Success == nil || config.Success.Path != "code" || string(config.Success.Value) != "0" {
		t.Fatal("Sub2API response defaults do not match its numeric code")
	}
	config.Submit.Headers = []CheckinHeader{{Name: "User-Agent", Value: "different-session"}}
	if NormalizeCheckinSite(&config) == nil {
		t.Fatal("per-request User-Agent can split a bound session")
	}
	if ValidateCheckinCredentials(CheckinCredentials{Type: "cookie", Cookie: "session=x", RefreshToken: "rt"}) == nil {
		t.Fatal("Cookie account accepted a refresh token")
	}
	if ValidateCheckinCredentials(CheckinCredentials{Type: "bearer", AccessToken: "access", RefreshToken: strings.Repeat("r", 16385)}) == nil {
		t.Fatal("unbounded refresh token accepted")
	}
}
