package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func requestNotificationTest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/notification/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	testNotificationChannel(c)
	return w
}

func TestNotificationTestSendsOnlySelectedDraftChannel(t *testing.T) {
	var webhookCalls, barkCalls atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/webhook" {
			webhookCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		barkCalls.Add(1)
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	defer sink.Close()
	body, _ := json.Marshal(map[string]any{"channel": "bark", "config": map[string]string{"webhook_url": sink.URL + "/webhook", "bark_url": sink.URL + "/bark"}})
	w := requestNotificationTest(t, string(body))
	if w.Code != http.StatusOK || webhookCalls.Load() != 0 || barkCalls.Load() != 1 {
		t.Fatalf("test did not isolate the selected channel: %d %s", w.Code, w.Body.String())
	}
}

func TestNotificationTestRejectsInvalidInputAndHidesProviderErrors(t *testing.T) {
	for _, body := range []string{
		`{"channel":"webhook","config":{"webhook_url":"https://example.invalid","templates":{"webhook":{"body":"{{password}}"}}}}`,
		`{"channel":"webhook","config":{"webhook_url":"https://example.invalid","templates":{"webhook":{"format":"html"}}}}`,
		`{`, `{"channel":"webhook","config":{"webhook_url":"file:///secret-marker"}}`,
		`{"channel":"unknown","config":{}}`, `{"channel":"telegram","config":{"telegram_bot_token":"123:secret-marker"}}`,
	} {
		w := requestNotificationTest(t, body)
		if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "secret-marker") {
			t.Fatalf("invalid request accepted or secret exposed: %d %s", w.Code, w.Body.String())
		}
	}
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":400,"message":"secret-marker"}`))
	}))
	defer sink.Close()
	body, _ := json.Marshal(map[string]any{"channel": "bark", "config": map[string]string{"bark_url": sink.URL + "/secret-marker"}})
	w := requestNotificationTest(t, string(body))
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "secret-marker") {
		t.Fatalf("provider failure was accepted or exposed: %d %s", w.Code, w.Body.String())
	}
}
