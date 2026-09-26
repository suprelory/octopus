package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

func setupSiteHandlerTestDB(t *testing.T) context.Context {
	t.Helper()
	if db.GetDB() != nil {
		_ = db.Close()
	}
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "site-handler.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background()
}

func requestSiteMutation(t *testing.T, path string, handler gin.HandlerFunc, payload map[string]any) (*httptest.ResponseRecorder, resp.ResponseStruct) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	var response resp.ResponseStruct
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	return w, response
}

func TestSiteNameConflictResponse(t *testing.T) {
	tests := []struct {
		name     string
		archived bool
		disabled bool
		update   bool
	}{
		{name: "create/active"},
		{name: "create/archived", archived: true},
		{name: "create_disabled/active", disabled: true},
		{name: "create_disabled/archived", archived: true, disabled: true},
		{name: "rename/active", update: true},
		{name: "rename/archived", archived: true, update: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := setupSiteHandlerTestDB(t)
			existing := model.Site{Name: "方舟", Platform: model.SitePlatformNewAPI, BaseURL: "https://existing.example", Enabled: true}
			if err := op.SiteCreate(&existing, ctx); err != nil {
				t.Fatal(err)
			}
			if tt.archived {
				if err := op.SiteArchive(existing.ID, ctx); err != nil {
					t.Fatal(err)
				}
			}

			path, handler := "/api/v1/site/create", gin.HandlerFunc(createSite)
			payload := map[string]any{
				"name": "  方舟  ", "platform": "new-api", "base_url": "https://new.example", "enabled": !tt.disabled,
			}
			var subject model.Site
			if tt.update {
				subject = model.Site{Name: "另一个站点", Platform: model.SitePlatformNewAPI, BaseURL: "https://new.example", Enabled: true}
				if err := op.SiteCreate(&subject, ctx); err != nil {
					t.Fatal(err)
				}
				path, handler = "/api/v1/site/update", updateSite
				payload = map[string]any{"id": subject.ID, "name": "  方舟  "}
			}

			w, response := requestSiteMutation(t, path, handler, payload)
			wantCode := op.CodeSiteNameExists
			if tt.archived {
				wantCode = op.CodeSiteArchivedNameExists
			}
			if w.Code != http.StatusConflict || response.Code != http.StatusConflict || response.ErrorCode != wantCode {
				t.Fatalf("expected 409 with %s, got HTTP %d: %s", wantCode, w.Code, w.Body.String())
			}
			if response.Params["name"] != existing.Name || response.Params["site_id"] != float64(existing.ID) {
				t.Fatalf("missing conflicting site details: %#v", response.Params)
			}
			if strings.Contains(response.Message, "UNIQUE") || strings.Contains(response.Message, "sites.name") {
				t.Fatalf("response leaked a database error: %s", response.Message)
			}

			var saved model.Site
			if err := db.GetDB().First(&saved, existing.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.Name != existing.Name || saved.Archived != tt.archived || saved.BaseURL != existing.BaseURL {
				t.Fatalf("conflicting site changed: %+v", saved)
			}
			if tt.update {
				var unchanged model.Site
				if err := db.GetDB().First(&unchanged, subject.ID).Error; err != nil || unchanged.Name != subject.Name {
					t.Fatalf("failed rename changed the source site: name=%s, err=%v", unchanged.Name, err)
				}
				sameName := "  " + subject.Name + "  "
				updated, err := op.SiteUpdate(&model.SiteUpdateRequest{ID: subject.ID, Name: &sameName}, ctx)
				if err != nil || updated.Name != subject.Name {
					t.Fatalf("saving a site's own name should succeed: %v", err)
				}
			} else {
				// A retry with a different name must still work, including the disabled-site transaction.
				payload["name"] = "方舟新站点"
				retry, _ := requestSiteMutation(t, path, handler, payload)
				if retry.Code != http.StatusOK {
					t.Fatalf("retry failed: HTTP %d: %s", retry.Code, retry.Body.String())
				}
			}
		})
	}
}

func TestCreateSiteOtherDatabaseErrorsRemainInternal(t *testing.T) {
	for _, name := range []string{"primary_key_conflict", "database_closed"} {
		t.Run(name, func(t *testing.T) {
			ctx := setupSiteHandlerTestDB(t)
			existing := model.Site{Name: "existing", Platform: model.SitePlatformNewAPI, BaseURL: "https://existing.example"}
			if err := op.SiteCreate(&existing, ctx); err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{
				"id": existing.ID, "name": "different-name", "platform": "new-api", "base_url": "https://new.example",
			}
			if name == "database_closed" {
				payload["name"] = existing.Name
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			w, response := requestSiteMutation(t, "/api/v1/site/create", createSite, payload)
			if w.Code != http.StatusInternalServerError || response.ErrorCode != apperror.CodeCommonInternalError {
				t.Fatalf("unexpected database error misclassified: HTTP %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
