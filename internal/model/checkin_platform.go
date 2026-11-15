package model

import "slices"

// Platform metadata is shared by validation and the management UI. Protocol
// implementations live in internal/checkin; credentials remain account-owned.
type CheckinPlatform struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Path             string   `json:"path"`
	CredentialTypes  []string `json:"credential_types"`
	Execution        string   `json:"execution"`
	StatusQuery      string   `json:"status_query"`
	CustomRequest    bool     `json:"custom_request"`
	CustomStatus     bool     `json:"custom_status"`
	TokenRefresh     bool     `json:"token_refresh"`
	AccountDetection bool     `json:"account_detection"`
	BalanceQuery     bool     `json:"balance_query"`
}

func CheckinPlatforms() []CheckinPlatform {
	return []CheckinPlatform{
		{ID: "new-api", Name: "New API", Path: "/api/user/checkin", CredentialTypes: []string{"cookie", "bearer"}, Execution: "request", StatusQuery: "native", CustomRequest: true, CustomStatus: true, AccountDetection: true, BalanceQuery: true},
		{ID: "one-api", Name: "One API", Path: "/api/user/checkin", CredentialTypes: []string{"cookie", "bearer"}, Execution: "request", StatusQuery: "configured", CustomRequest: true, CustomStatus: true},
		{ID: "one-hub", Name: "OneHub", Path: "/api/user/checkin", CredentialTypes: []string{"cookie", "bearer"}, Execution: "request", StatusQuery: "configured", CustomRequest: true, CustomStatus: true},
		{ID: "done-hub", Name: "DoneHub", Path: "/api/user/checkin", CredentialTypes: []string{"cookie", "bearer"}, Execution: "request", StatusQuery: "configured", CustomRequest: true, CustomStatus: true},
		{ID: "sub2api", Name: "Sub2API", CredentialTypes: []string{"bearer", "cookie"}, Execution: "request", StatusQuery: "configured", CustomRequest: true, CustomStatus: true, TokenRefresh: true, AccountDetection: true, BalanceQuery: true},
		{ID: "anyrouter", Name: "AnyRouter", Path: "/api/user/sign_in", CredentialTypes: []string{"cookie", "bearer", "password"}, Execution: "request", StatusQuery: "configured", CustomStatus: true},
		{ID: "agentrouter", Name: "AgentRouter", Path: "/api/user/login", CredentialTypes: []string{"password"}, Execution: "login", StatusQuery: "logs"},
		{ID: "http", Name: "Custom HTTP", CredentialTypes: []string{"cookie", "bearer"}, Execution: "request", StatusQuery: "configured", CustomRequest: true, CustomStatus: true},
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
