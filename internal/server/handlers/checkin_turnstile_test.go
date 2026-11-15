package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/server/auth"
)

func TestTurnstileAPIEncryptsKeysAndRequiresExplicitEndpointKeyReplacement(t *testing.T) {
	engine := testCheckinEngine(t)
	var calls atomic.Int32
	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer solver.Close()
	token, _, err := auth.GenerateJWTToken(60)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method string, body any, authenticated bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/checkin/turnstile", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "auth", Value: token})
		} else {
			r.Header.Set("Authorization", "Bearer sk-channel-key")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "super-private-key") || strings.Contains(w.Body.String(), "key_cipher") {
			t.Fatal("solver key leaked through the API")
		}
		return w
	}
	for _, method := range []string{"GET", "PATCH"} {
		if response := call(method, nil, false); response.Code != 401 {
			t.Fatal("non-admin accessed solver settings", response.Code)
		}
	}
	response := call("GET", nil, true)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"revision":0`) {
		t.Fatal("default settings unavailable")
	}
	config := model.DefaultCheckinTurnstileSettings().Config
	config.Enabled, config.CloudflareSolver.Enabled, config.CloudflareSolver.BaseURL = true, true, solver.URL
	key := "super-private-key"
	response = call("PATCH", checkin.TurnstileWrite{Config: config, CloudflareKey: &key}, true)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"has_cloudflare_key":true`) {
		t.Fatal("key save failed", response.Code, response.Body)
	}
	stored, _ := checkin.Default.Store.TurnstileSettings(context.Background())
	if stored.CloudflareKeyCipher == "" || strings.Contains(stored.CloudflareKeyCipher, key) {
		t.Fatal("key was stored without encryption")
	}
	if response := call("GET", nil, true); response.Code != 200 {
		t.Fatal("saved settings unavailable")
	}
	config.Enabled = false
	if response := call("PATCH", checkin.TurnstileWrite{Config: config, Revision: 1}, true); response.Code != 200 {
		t.Fatal("blank edit did not retain key")
	}
	current, _ := checkin.Default.Store.TurnstileSettings(context.Background())
	if current.CloudflareKeyCipher != stored.CloudflareKeyCipher {
		t.Fatal("metadata update replaced key")
	}
	if response := call("PATCH", checkin.TurnstileWrite{Config: config, Revision: 1}, true); response.Code != 409 {
		t.Fatal("stale config update was accepted")
	}
	config.CloudflareSolver.BaseURL = solver.URL + "/different"
	if response := call("PATCH", checkin.TurnstileWrite{Config: config, Revision: 2}, true); response.Code != 400 {
		t.Fatal("saved key was silently redirected to a changed endpoint")
	}
	empty := ""
	if response := call("PATCH", checkin.TurnstileWrite{Config: config, Revision: 2, CloudflareKey: &empty}, true); response.Code != 200 {
		t.Fatal("explicit key clearing failed")
	}
	current, _ = checkin.Default.Store.TurnstileSettings(context.Background())
	if current.HasCloudflareKey || current.CloudflareKeyCipher != "" {
		t.Fatal("key was not cleared")
	}
	config.Enabled = true
	if response := call("PATCH", checkin.TurnstileWrite{Config: config, Revision: 3}, true); response.Code != 400 {
		t.Fatal("enabled solver accepted a missing key")
	}
	if response := call("PATCH", map[string]any{"config": config, "revision": 3, "cloudflare_key_cipher": "forged"}, true); response.Code != 400 {
		t.Fatal("API accepted forged ciphertext")
	}
	if calls.Load() != 0 {
		t.Fatal("settings operations called a paid solver")
	}
}
