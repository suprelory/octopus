package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
)

func TestProxyAPIAuthenticationPrivacyAndChannelModelProbe(t *testing.T) {
	engine := testCheckinEngine(t)
	if err := db.InitDB("sqlite", filepath.ToSlash(filepath.Join(t.TempDir(), "proxy-api.db")), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	t.Setenv("OCTOPUS_PROXY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)))
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/proxy-pool") {
			continue
		}
		request := httptest.NewRequest(route.Method, strings.ReplaceAll(route.Path, ":id", "1"), strings.NewReader("{}"))
		request.Header.Set("Authorization", "Bearer relay-api-key")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatal("proxy management accepts relay API keys", route, response.Code)
		}
	}
	token, _, err := auth.GenerateJWTToken(60)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "auth", Value: token})
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	var probes atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("api-user:api-proxy-secret")) {
			t.Error("model probe lost proxy credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer proxy.Close()
	response := call("POST", "/api/v1/proxy-pool", model.ProxyWrite{Name: "API proxy", URL: strings.Replace(proxy.URL, "http://", "http://api-user:api-proxy-secret@", 1), Enabled: true})
	if response.Code != 200 || strings.Contains(response.Body.String(), "api-proxy-secret") || strings.Contains(response.Body.String(), "auth_cipher") {
		t.Fatal("proxy create failed or leaked credentials", response.Code)
	}
	var envelope struct{ Data model.ProxyConfiguration }
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	node := envelope.Data
	if node.ID == 0 || !node.HasAuth {
		t.Fatal("proxy API omitted state")
	}
	displayURL := strings.Replace(proxy.URL, "http://", "http://api-user:***@", 1)
	if node.URL != proxy.URL || node.DisplayURL != displayURL {
		t.Fatal("proxy API did not separate the endpoint and masked display URL")
	}
	response = call("GET", "/api/v1/proxy-pool", nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "auth_cipher") || strings.Contains(response.Body.String(), "api-proxy-secret") {
		t.Fatal("proxy list exposes secrets")
	}
	var list struct{ Data []model.ProxyConfiguration }
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil || len(list.Data) != 1 || list.Data[0].DisplayURL != displayURL || list.Data[0].URL != proxy.URL {
		t.Fatal("proxy list omitted the username or changed the endpoint", err)
	}
	response = call("PATCH", "/api/v1/proxy-pool/1", map[string]any{
		"name": node.Name, "url": node.URL, "enabled": true, "revision": node.Revision, "display_url": node.DisplayURL,
	})
	if response.Code != 400 {
		t.Fatal("proxy API accepted the read-only display URL", response.Code)
	}
	response = call("PATCH", "/api/v1/proxy-pool/1", model.ProxyWrite{Name: "Renamed API proxy", URL: node.URL, Enabled: true, Revision: node.Revision})
	if response.Code != 200 || strings.Contains(response.Body.String(), "api-proxy-secret") {
		t.Fatal("proxy edit failed or leaked credentials", response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.DisplayURL != displayURL || !envelope.Data.HasAuth {
		t.Fatal("proxy edit lost the masked display URL", err)
	}
	node = envelope.Data
	config := model.ChannelConfig{Name: "Pool channel", BaseURL: "http://upstream.invalid", ProxyMode: "pool", ProxyConfigID: &node.ID}
	response = call("POST", "/api/v1/channel/fetch-model", model.ChannelFetchModelRequest{Channel: config, Key: "upstream-test-key"})
	if response.Code != 200 || probes.Load() != 2 || !strings.Contains(response.Body.String(), "test-model") {
		t.Fatal("unsaved model probe bypassed pool", response.Code, response.Body.String())
	}
	channel := model.Channel{ChannelConfig: config}
	if err := db.GetDB().Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	response = call("DELETE", "/api/v1/proxy-pool/1", nil)
	if response.Code != 409 {
		t.Fatal("referenced proxy deletion accepted", response.Code)
	}
	if _, err := op.ProxySave(context.Background(), db.GetDB(), node.ID, model.ProxyWrite{Name: node.Name, URL: node.URL, Enabled: false, Revision: node.Revision}); err != nil {
		t.Fatal(err)
	}
	response = call("POST", "/api/v1/channel/fetch-model", model.ChannelFetchModelRequest{Channel: config, Key: "upstream-test-key"})
	if response.Code != 502 || probes.Load() != 2 {
		t.Fatal("disabled model probe used cached proxy", response.Code)
	}
}
