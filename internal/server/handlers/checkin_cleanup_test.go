package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/google/uuid"
)

func TestCheckinCleanupAPI(t *testing.T) {
	engine := testCheckinEngine(t)
	token, _, err := auth.GenerateJWTToken(60)
	if err != nil {
		t.Fatal(err)
	}
	job := model.CheckinJob{ID: uuid.NewString(), IdempotencyKey: uuid.NewString(), Status: "completed", Source: "manual"}
	if err := checkin.Default.Store.DB.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/checkin"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			req.AddCookie(&http.Cookie{Name: "auth", Value: token})
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	for _, route := range []struct{ method, path string }{{"POST", "/jobs/cleanup/preview"}, {"POST", "/jobs/cleanup"}, {"GET", "/jobs/cleanup/status"}} {
		if r := call(route.method, route.path, "{}", false); r.Code != 401 {
			t.Fatal("cleanup route lacks authentication", route, r.Code)
		}
	}
	for _, body := range []string{`{}`, `{"job_ids":[]}`, `{"job_ids":["bad"]}`, `{"before":"tomorrow"}`, `{"all":true}`, `null`} {
		for _, path := range []string{"/jobs/cleanup/preview", "/jobs/cleanup"} {
			if r := call("POST", path, body, true); r.Code != 400 {
				t.Fatal("invalid cleanup request accepted", body, r.Code, r.Body.String())
			}
		}
	}
	for _, query := range []string{"limit=oops", "limit=0", "limit=101", "before=bad", "source=bad", "status=bad", "from=bad"} {
		if r := call("GET", "/jobs?"+query, "", true); r.Code != 400 {
			t.Fatal("invalid pagination accepted", query, r.Code)
		}
	}
	r := call("GET", "/jobs?limit=1&source=manual&status=completed", "", true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"items":[`) || !strings.Contains(r.Body.String(), job.ID) {
		t.Fatal("job page response invalid", r.Body.String())
	}
	body := `{"job_ids":["` + job.ID + `"]}`
	r = call("POST", "/jobs/cleanup/preview", body, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"jobs":1`) {
		t.Fatal("cleanup preview failed", r.Body.String())
	}
	if _, _, err := checkin.Default.Store.Job(context.Background(), job.ID); err != nil {
		t.Fatal("preview deleted job", err)
	}
	r = call("POST", "/jobs/cleanup", body, true)
	var result struct {
		Data model.CheckinCleanupReport `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || r.Code != 200 || result.Data.Status != "completed" || result.Data.Counts.Jobs != 1 {
		t.Fatal("cleanup failed", r.Body.String(), err)
	}
	r = call("GET", "/jobs/cleanup/status", "", true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"source":"manual"`) {
		t.Fatal("missing cleanup report", r.Body.String())
	}
	if r := call("GET", "/jobs/"+job.ID, "", true); r.Code != 404 {
		t.Fatal("deleted job still available", r.Code)
	}
	for _, body := range []string{`{"auto_cleanup":true,"retention_days":180}`, `{"concurrency":3}`} {
		if r := call("PATCH", "/settings", body, true); r.Code != 200 {
			t.Fatal("partial settings write failed", r.Body.String())
		}
	}
	settings, err := checkin.Default.Store.Settings(context.Background())
	if err != nil || !settings.AutoCleanup || settings.RetentionDays != 180 || settings.Concurrency != 3 {
		t.Fatal("editing execution settings reset retention", settings, err)
	}
	for _, body := range []string{`{"unknown":true}`, `{"auto_cleanup":true,"retention_days":0}`, `{"retention_days":3651}`} {
		if r := call("PATCH", "/settings", body, true); r.Code != 400 {
			t.Fatal("invalid retention settings accepted", r.Code, r.Body.String())
		}
	}
}
