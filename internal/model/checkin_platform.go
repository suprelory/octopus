package model

import "slices"

// Platform metadata is shared by validation and the management UI. Protocol
// implementations live in internal/checkin; credentials remain account-owned.
type CheckinPlatform struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	CredentialTypes []string `json:"credential_types"`
	Execution       string   `json:"execution"`
	StatusQuery     string   `json:"status_query"`
	CustomRequest   bool     `json:"custom_request"`
	CustomStatus    bool     `json:"custom_status"`
	TokenRefresh    bool     `json:"token_refresh"`
}

func CheckinPlatforms() []CheckinPlatform {
	return []CheckinPlatform{
		{"new-api", "New API", "/api/user/checkin", []string{"cookie", "bearer"}, "request", "native", true, true, false},
		{"one-api", "One API", "/api/user/checkin", []string{"cookie", "bearer"}, "request", "configured", true, true, false},
		{"one-hub", "OneHub", "/api/user/checkin", []string{"cookie", "bearer"}, "request", "configured", true, true, false},
		{"done-hub", "DoneHub", "/api/user/checkin", []string{"cookie", "bearer"}, "request", "configured", true, true, false},
		{"sub2api", "Sub2API", "", []string{"bearer", "cookie"}, "request", "configured", true, true, true},
		{"anyrouter", "AnyRouter", "/api/user/sign_in", []string{"cookie", "bearer", "password"}, "request", "configured", false, true, false},
		{"agentrouter", "AgentRouter", "/api/user/login", []string{"password"}, "login", "logs", false, false, false},
		{"http", "Custom HTTP", "", []string{"cookie", "bearer"}, "request", "configured", true, true, false},
	}
}

func CheckinPlatformByID(id string) (CheckinPlatform, bool) {
	for _, platform := range CheckinPlatforms() {
		if platform.ID == id {
			return platform, true
		}
	}
	return CheckinPlatform{}, false
}

func CheckinCredentialSupported(platform, kind string) bool {
	info, ok := CheckinPlatformByID(platform)
	return ok && slices.Contains(info.CredentialTypes, kind)
}

// Explicit status rules and custom submit routes retain their existing behavior.
// The native New API contract applies only to its standard check-in endpoint.
func CheckinNativeStatus(c CheckinSiteConfig) bool {
	return c.Adapter == "agentrouter" || (c.Adapter == "new-api" && c.Status == nil && c.StatusMode != "disabled" && c.Submit.Method == "POST" && c.Submit.Path == "/api/user/checkin")
}
