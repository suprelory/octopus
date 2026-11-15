package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var checkinRoutesOnce sync.Once
var checkinRoutes *gin.Engine

func testCheckinEngine(t *testing.T) *gin.Engine {
	t.Helper()
	checkinRoutesOnce.Do(func() {
		gin.SetMode(gin.TestMode)
		checkinRoutes = gin.New()
		if err := router.RegisterAll(checkinRoutes); err != nil {
			t.Fatal(err)
		}
		checkinRoutes.GET("/test-checkin-error", func(c *gin.Context) {
			withCheckin(c, 200, func(*checkin.Service) (any, error) { return nil, errors.New("SQL error contains secret-credential") })
		})
	})
	conn, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "api.db"))+"?_pragma=foreign_keys(ON)"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(&model.CheckinSite{}, &model.CheckinAccount{}, &model.CheckinRun{}, &model.CheckinAttempt{}, &model.CheckinJob{}, &model.CheckinJobItem{}, &model.CheckinNotificationTarget{}, &model.CheckinNotificationDelivery{}, &model.CheckinTurnstileSettings{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	vault, _ := checkinsecret.New(bytes.Repeat([]byte{2}, 32))
	previous := checkin.Default
	checkin.Default = checkin.New(op.NewCheckinStore(conn), vault)
	t.Cleanup(func() {
		_ = checkin.Default.Close()
		checkin.Default = previous
		sqlDB, _ := conn.DB()
		_ = sqlDB.Close()
	})
	return checkinRoutes
}

func TestCheckinInspectionAPIIsPrivateAndDoesNotAcceptCustomRequests(t *testing.T) {
	engine := testCheckinEngine(t)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/api/user/self" || r.Header.Get("Authorization") != "Bearer private-inspection-token" {
			t.Error("API inspection invoked an unexpected upstream action")
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":42,"quota":0,"used_quota":9007199255,"email":"private-profile@example.invalid"}}`))
	}))
	defer upstream.Close()
	settings := model.DefaultCheckinSettings()
	settings.SiteIntervalSeconds = 0
	if err := checkin.Default.Store.SaveSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	site, err := checkin.Default.Store.SaveSite(context.Background(), "", 0, model.CheckinSiteConfig{Name: "Read-only API", BaseURL: upstream.URL, Adapter: "new-api", Enabled: true, StatusMode: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Account"
	account, err := checkin.Default.SaveAccount(context.Background(), site.ID, "", checkin.AccountWrite{Name: &name, Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: "private-inspection-token"}})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.GenerateJWTToken(60)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "auth", Value: token})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	for _, action := range []string{"balance", "detect"} {
		path := "/api/v1/checkin/accounts/" + account.ID + "/" + action
		before := requests.Load()
		if response := call("POST", path, `{"path":"/api/user/login","method":"POST"}`); response.Code != 400 || requests.Load() != before {
			t.Fatal("inspection accepted an injected request")
		}
		if response := call("GET", path, ""); response.Code != 404 || requests.Load() != before {
			t.Fatal("a GET navigation triggered token renewal/inspection")
		}
		response := call("POST", path, "{}")
		if response.Code != 200 || requests.Load() != before+1 || !strings.Contains(response.Body.String(), `"amount_micros":"0"`) || !strings.Contains(response.Body.String(), `"used_micros":"9007199255000000"`) || strings.Contains(response.Body.String(), "private-") || strings.Contains(response.Body.String(), "credential_cipher") {
			t.Fatal("inspection response violated its public amount/privacy contract", response.Code)
		}
	}
	if response := call("POST", "/api/v1/checkin/accounts/not-an-id/balance", "{}"); response.Code != 400 {
		t.Fatal("invalid account ID accepted")
	}
	if response := call("PATCH", "/api/v1/checkin/accounts/"+account.ID, `{"revision":1,"balance":{"amount_micros":"999"}}`); response.Code != 400 {
		t.Fatal("account write accepted caller-supplied balance")
	}
	disabled := false
	if _, err := checkin.Default.SaveAccount(context.Background(), site.ID, account.ID, checkin.AccountWrite{Revision: account.Revision, Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if response := call("POST", "/api/v1/checkin/accounts/"+account.ID+"/detect", "{}"); response.Code != 400 || requests.Load() != 2 {
		t.Fatal("disabled account still performed a network request")
	}
}

func TestCheckinRoutesRequireAdminCookieAndKeepCredentialsPrivate(t *testing.T) {
	engine := testCheckinEngine(t)
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/checkin") {
			continue
		}
		path := strings.ReplaceAll(route.Path, ":id", uuid.NewString())
		request := httptest.NewRequest(route.Method, path, strings.NewReader("{}"))
		request.Header.Set("Authorization", "Bearer relay-api-key")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("%s %s accepted without admin cookie: %d", route.Method, path, response.Code)
		}
	}
	token, _, err := auth.GenerateJWTToken(60)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any, key string) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		request.AddCookie(&http.Cookie{Name: "auth", Value: token})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	decode := func(response *httptest.ResponseRecorder, dest any) {
		t.Helper()
		var envelope struct{ Data json.RawMessage }
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(envelope.Data, dest); err != nil {
			t.Fatal(err)
		}
	}
	config := model.CheckinSiteConfig{Name: "API site", BaseURL: "https://example.invalid", Adapter: "new-api", Enabled: true}
	response := call("POST", "/api/v1/checkin/sites", map[string]any{"config": config}, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var site model.CheckinSite
	decode(response, &site)
	unknown := call("POST", "/api/v1/checkin/sites", map[string]any{"config": config, "linked_site_id": 1}, "")
	if unknown.Code != 400 {
		t.Fatal("subscription association was accepted")
	}
	accountResponse := call("POST", "/api/v1/checkin/sites/"+site.ID+"/accounts", map[string]any{"name": "API account", "credentials": map[string]string{"type": "bearer", "access_token": "secret-credential", "user_id": "123"}}, "")
	if accountResponse.Code != 200 {
		t.Fatal(accountResponse.Body.String())
	}
	var account model.CheckinAccount
	decode(accountResponse, &account)
	if strings.Contains(accountResponse.Body.String(), "secret-credential") || strings.Contains(accountResponse.Body.String(), "credential_cipher") || account.CredentialUserID != "123" {
		t.Fatal("account credential response unsafe")
	}
	unknown = call("PATCH", "/api/v1/checkin/accounts/"+account.ID, map[string]any{"revision": account.Revision, "linked_account_id": 1}, "")
	if unknown.Code != 400 {
		t.Fatal("linked account reference accepted")
	}
	updated := call("PATCH", "/api/v1/checkin/accounts/"+account.ID, map[string]any{"revision": account.Revision, "name": "Renamed"}, "")
	if updated.Code != 200 {
		t.Fatal(updated.Body.String())
	}
	stale := call("PATCH", "/api/v1/checkin/accounts/"+account.ID, map[string]any{"revision": account.Revision, "name": "Stale"}, "")
	if stale.Code != 409 {
		t.Fatal("stale revision not rejected")
	}

	scope := model.CheckinScope{Kind: "accounts", IDs: []string{account.ID}}
	first := call("POST", "/api/v1/checkin/jobs", scope, "same-click")
	repeat := call("POST", "/api/v1/checkin/jobs", scope, "same-click")
	if first.Code != 202 || repeat.Code != 202 {
		t.Fatal("job not accepted asynchronously")
	}
	var a, b model.CheckinJob
	decode(first, &a)
	decode(repeat, &b)
	if a.ID != b.ID {
		t.Fatal("API idempotency failed")
	}
	details := call("GET", "/api/v1/checkin/jobs/"+a.ID, nil, "")
	if details.Code != 200 || !strings.Contains(details.Body.String(), "Renamed") {
		t.Fatal("job snapshots missing")
	}
	cancel := call("POST", "/api/v1/checkin/jobs/"+a.ID+"/cancel", nil, "")
	if cancel.Code != 204 {
		t.Fatal("cancel failed")
	}
	jobs, err := checkin.Default.Store.Jobs(context.Background())
	if err != nil || jobs[0].Status != "canceled" || jobs[0].Completed != 1 {
		t.Fatal("cancel progress not reflected")
	}
	if response := call("GET", "/api/v1/checkin/runs?limit=1000", nil, ""); response.Code != 400 {
		t.Fatal("unbounded history accepted")
	}
	if response := call("GET", "/test-checkin-error", nil, ""); response.Code != 500 || strings.Contains(response.Body.String(), "secret-credential") {
		t.Fatal("internal error leaked")
	}
	preview := call("POST", "/api/v1/checkin/rules/test", map[string]any{"config": site.Config, "response": map[string]any{"success": true, "data": map[string]any{"quota_awarded": 0}}, "date": "2030-11-14", "http_status": 200}, "")
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), `"reward":"0"`) {
		t.Fatal("offline rule preview failed", preview.Body.String())
	}
}

func TestCheckinJSONRejectsTextAndUnknownCredentialFields(t *testing.T) {
	engine := testCheckinEngine(t)
	token, _, _ := auth.GenerateJWTToken(60)
	for _, entry := range []struct{ contentType, body string }{
		{"text/plain", `{"config":{}}`},
		{"application/json", `{"config":{}} {}`},
		{"application/json", `{"config":{"password":"shared-site-secret"}}`},
	} {
		request := httptest.NewRequest("POST", "/api/v1/checkin/sites", strings.NewReader(entry.body))
		request.Header.Set("Content-Type", entry.contentType)
		request.AddCookie(&http.Cookie{Name: "auth", Value: token})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != 400 || strings.Contains(response.Body.String(), "shared-site-secret") {
			t.Fatal("unsafe request accepted or echoed")
		}
	}
}

func TestSub2APICredentialsAPIKeepsTokensPrivateAndClearsOldExpiry(t *testing.T) {
	engine := testCheckinEngine(t)
	token, _, _ := auth.GenerateJWTToken(60)
	call := func(method, path string, body any, output any) int {
		t.Helper()
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "auth", Value: token})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), "private-access") || strings.Contains(response.Body.String(), "private-refresh") || strings.Contains(response.Body.String(), "credential_cipher") {
			t.Fatal("API exposed a session secret")
		}
		if output != nil && response.Code == 200 {
			var envelope struct{ Data json.RawMessage }
			if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || json.Unmarshal(envelope.Data, output) != nil {
				t.Fatal("invalid API response")
			}
		}
		return response.Code
	}
	var site model.CheckinSite
	config := model.CheckinSiteConfig{Name: "Sub2API", BaseURL: "https://example.invalid", Adapter: "sub2api", Enabled: true, Submit: model.CheckinRequest{Path: "/checkin"}}
	if call("POST", "/api/v1/checkin/sites", map[string]any{"config": config}, &site) != 200 {
		t.Fatal("Sub2API site rejected")
	}
	expiry := time.Now().UTC().Add(time.Hour)
	var account model.CheckinAccount
	if call("POST", "/api/v1/checkin/sites/"+site.ID+"/accounts", map[string]any{"name": "session", "credentials": model.CheckinCredentials{Type: "bearer", AccessToken: "private-access", RefreshToken: "private-refresh", ExpiresAt: &expiry}}, &account) != 200 {
		t.Fatal("session credentials rejected")
	}
	if !account.HasRefreshToken || account.TokenExpiresAt == nil || account.AutoCheckin {
		t.Fatal("session metadata or default automation is incorrect")
	}
	oldVersion := account.CredentialVersion
	var replacement model.CheckinAccount
	if call("PATCH", "/api/v1/checkin/accounts/"+account.ID, map[string]any{"revision": account.Revision, "credential_version": oldVersion, "credentials": model.CheckinCredentials{Type: "bearer", AccessToken: "replacement"}}, &replacement) != 200 {
		t.Fatal("replacement credentials rejected")
	}
	if replacement.HasRefreshToken || replacement.TokenExpiresAt != nil || replacement.CredentialVersion != oldVersion+1 {
		t.Fatal("replacement retained an old refresh token or expiry")
	}
	config.Adapter = "http"
	if call("POST", "/api/v1/checkin/sites", map[string]any{"config": config}, &site) != 200 {
		t.Fatal("custom site rejected")
	}
	if call("POST", "/api/v1/checkin/sites/"+site.ID+"/accounts", map[string]any{"name": "wrong platform", "credentials": model.CheckinCredentials{Type: "bearer", AccessToken: "private-access", RefreshToken: "private-refresh"}}, nil) != 400 {
		t.Fatal("another platform accepted Sub2API refresh credentials")
	}
}

func TestRouterCapabilitiesAndEncryptedPasswordAccountAPI(t *testing.T) {
	engine := testCheckinEngine(t)
	token, _, _ := auth.GenerateJWTToken(60)
	call := func(method, path string, body any, output any) int {
		t.Helper()
		data, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "auth", Value: token})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), "private-router") || strings.Contains(response.Body.String(), "credential_cipher") {
			t.Fatal("router API leaked encrypted credential content")
		}
		if output != nil && response.Code == 200 {
			var envelope struct{ Data json.RawMessage }
			if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || json.Unmarshal(envelope.Data, output) != nil {
				t.Fatal("invalid API response")
			}
		}
		return response.Code
	}
	var capabilities []checkin.Capability
	if call("GET", "/api/v1/checkin/capabilities", nil, &capabilities) != 200 {
		t.Fatal("capability request failed")
	}
	var agent *checkin.Capability
	for i := range capabilities {
		if capabilities[i].ID == "agentrouter" {
			agent = &capabilities[i]
		}
	}
	if agent == nil || agent.Execution != "login" || agent.StatusQuery != "logs" || agent.CustomRequest || agent.CustomStatus || len(agent.CredentialTypes) != 1 || agent.CredentialTypes[0] != "password" {
		t.Fatal("incorrect AgentRouter capabilities")
	}
	var site model.CheckinSite
	config := model.CheckinSiteConfig{Name: "AgentRouter", BaseURL: "https://example.invalid", Adapter: "agentrouter", Enabled: true}
	if call("POST", "/api/v1/checkin/sites", map[string]any{"config": config}, &site) != 200 {
		t.Fatal("AgentRouter site rejected")
	}
	accountPath := "/api/v1/checkin/sites/" + site.ID + "/accounts"
	if call("POST", accountPath, map[string]any{"name": "Invalid", "credentials": model.CheckinCredentials{Type: "cookie", Cookie: "private-router-cookie"}}, nil) != 400 {
		t.Fatal("AgentRouter accepted a cookie-only account")
	}
	var account model.CheckinAccount
	credentials := model.CheckinCredentials{Type: "password", Username: "private-router-user", Password: "private-router-password"}
	if call("POST", accountPath, map[string]any{"name": "Dedicated login", "credentials": credentials}, &account) != 200 || account.CredentialType != "password" {
		t.Fatal("password account rejected")
	}
	if call("PATCH", "/api/v1/checkin/accounts/"+account.ID, map[string]any{"revision": account.Revision, "name": "Renamed"}, &account) != 200 {
		t.Fatal("metadata edit erased credentials")
	}
	stored, err := checkin.Default.Store.Account(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := checkin.Default.Vault.Unseal(stored.ID, stored.CredentialCipher)
	if err != nil || !strings.Contains(string(plain), "private-router-password") {
		t.Fatal("password was not stored in the vault")
	}
	credentials.Session = &model.CheckinLoginSession{BaseURL: site.Config.BaseURL, Auth: "bearer", AccessToken: "private-router-session"}
	if call("PATCH", "/api/v1/checkin/accounts/"+account.ID, map[string]any{"revision": account.Revision, "credential_version": account.CredentialVersion, "credentials": credentials}, nil) != 400 {
		t.Fatal("API accepted a forged derived session")
	}
	call("GET", accountPath, nil, nil)
}
