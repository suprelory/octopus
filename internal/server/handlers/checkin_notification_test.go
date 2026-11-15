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
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/server/auth"
)

func TestNotificationAPIProtectsConnectionsAndSeparatesPreviewFromSending(t *testing.T) {
	engine := testCheckinEngine(t)
	var calls atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer sink.Close()
	token, _, err := auth.GenerateJWTToken(60)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/checkin"+path, strings.NewReader(string(data)))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "auth", Value: token})
		} else {
			r.Header.Set("Authorization", "Bearer sk-channel-key")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, r)
		return response
	}
	for _, route := range []struct{ method, path string }{{"GET", "/notifications"}, {"POST", "/notifications"}, {"POST", "/notification-preview"}, {"GET", "/notification-deliveries"}} {
		if response := call(route.method, route.path, nil, false); response.Code != 401 {
			t.Fatalf("notification API bypassed admin authentication: %d", response.Code)
		}
	}
	config := model.CheckinNotificationConfig{Name: "Private destination", Kind: "webhook", Enabled: false, Events: []string{"failed"}, Scope: "all", Language: "zh_hans"}
	response := call("POST", "/notifications", checkin.NotificationWrite{Config: config, Connection: &notify.Connection{URL: sink.URL + "/private-webhook-token"}}, true)
	if response.Code != 200 || strings.Contains(response.Body.String(), "private-webhook-token") || strings.Contains(response.Body.String(), "connection_cipher") {
		t.Fatal("notification save leaked its connection", response.Code)
	}
	var envelope struct {
		Data model.CheckinNotificationTarget `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	target := envelope.Data
	if target.ID == "" || !target.HasConnection {
		t.Fatal("saved target response was incomplete")
	}
	for _, path := range []string{"/notifications/" + target.ID + "/test", "/notification-deliveries/" + target.ID + "/retry"} {
		if response := call("POST", path, map[string]int{"revision": 1}, false); response.Code != 401 {
			t.Fatal("private action accepted an API key")
		}
	}
	if response := call("GET", "/notifications", nil, true); response.Code != 200 || strings.Contains(response.Body.String(), "private-webhook-token") {
		t.Fatal("notification list leaked secrets")
	}
	preview := map[string]string{"language": "zh_hans", "title_template": "{{site}}", "body_template": "{{balance}} {{balance_unit}}"}
	if response := call("POST", "/notification-preview", preview, true); response.Code != 200 || calls.Load() != 0 {
		t.Fatal("template preview sent a notification")
	}
	if response := call("POST", "/notifications/"+target.ID+"/test", map[string]int{"revision": 1}, true); response.Code != 202 || calls.Load() != 0 {
		t.Fatal("test did not queue independently", response.Code)
	}
	if !checkin.Default.ProcessNotification(context.Background()) || calls.Load() != 1 {
		t.Fatal("disabled target could not send an explicit test")
	}
	if response := call("GET", "/notification-deliveries", nil, true); response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"sent"`) || strings.Contains(response.Body.String(), "private-webhook-token") {
		t.Fatal("delivery audit was missing or leaked a secret")
	}
	if response := call("PATCH", "/notifications/"+target.ID, checkin.NotificationWrite{Config: config, Revision: 1}, true); response.Code != 200 {
		t.Fatal("metadata edit did not retain the saved connection", response.Code)
	}
	if response := call("PATCH", "/notifications/"+target.ID, checkin.NotificationWrite{Config: config, Revision: 1}, true); response.Code != 409 {
		t.Fatal("stale edit was accepted")
	}
	if response := call("POST", "/notifications/"+target.ID+"/test", map[string]any{"revision": 2, "url": sink.URL}, true); response.Code != 400 {
		t.Fatal("test accepted a destination override")
	}
	if response := call("POST", "/notification-preview", map[string]string{"language": "en", "body_template": "{{access_token}}"}, true); response.Code != 400 {
		t.Fatal("template could access private data")
	}
	if response := call("GET", "/notification-deliveries?target_id=invalid", nil, true); response.Code != 400 {
		t.Fatal("invalid filter accepted")
	}
	if response := call("DELETE", "/notifications/"+target.ID, map[string]int{"revision": 2}, true); response.Code != 204 {
		t.Fatal("notification target could not be deleted", response.Code)
	}
}
