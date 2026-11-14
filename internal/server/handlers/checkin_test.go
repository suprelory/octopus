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
	"testing"

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
	if err := conn.AutoMigrate(&model.CheckinSite{}, &model.CheckinAccount{}, &model.CheckinRun{}, &model.CheckinAttempt{}, &model.CheckinJob{}, &model.CheckinJobItem{}, &model.Setting{}); err != nil {
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
	preview := call("POST", "/api/v1/checkin/rules/test", map[string]any{"config": site.Config, "response": map[string]any{"success": true, "data": map[string]any{"reward": 0}}, "date": "2030-11-14", "http_status": 200}, "")
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
