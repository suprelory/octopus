package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func requestCheckinLogs(t *testing.T, query string) (*httptest.ResponseRecorder, model.SiteCheckinLogPage) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/site/checkin-logs"+query, nil)
	listSiteCheckinLogs(c)
	var response struct {
		Data model.SiteCheckinLogPage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return w, response.Data
}

func TestCheckinLogQueryFiltersAndStablePagination(t *testing.T) {
	setupSiteHandlerTestDB(t)
	day := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	rows := []model.SiteCheckinLog{
		{ID: 100, SiteID: 1, AccountID: 11, SiteName: "Original name", AccountName: "Original account", Source: "manual", Status: model.SiteExecutionStatusSuccess, BatchJobID: 778, FinishedAt: day.Add(time.Hour)},
		{ID: 200, SiteID: 1, AccountID: 11, Source: "manual", Status: model.SiteExecutionStatusSuccess, FinishedAt: day.Add(2 * time.Hour)},
		{ID: 300, SiteID: 1, AccountID: 11, Source: "scheduled", Status: model.SiteExecutionStatusFailed, FinishedAt: day.Add(3 * time.Hour)},
		{ID: 400, SiteID: 2, AccountID: 22, Source: "manual", Status: model.SiteExecutionStatusSuccess, FinishedAt: day.Add(4 * time.Hour)},
		{ID: 500, SiteID: 1, AccountID: 11, Source: "manual", Status: model.SiteExecutionStatusSuccess, FinishedAt: day.Add(24 * time.Hour)},
	}
	if err := db.GetDB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	query := "?site_id=1&account_id=11&status=success&source=manual&from=2026-09-27T00:00:00Z&until=2026-09-28T00:00:00Z&limit=1"
	w, first := requestCheckinLogs(t, query)
	if w.Code != 200 || len(first.Items) != 1 || first.Items[0].ID != 200 || first.NextBeforeID != 200 {
		t.Fatalf("unexpected first page: %d %s", w.Code, w.Body.String())
	}
	inserted := rows[1]
	inserted.ID = 600
	if err := db.GetDB().Create(&inserted).Error; err != nil {
		t.Fatal(err)
	}
	w, second := requestCheckinLogs(t, query+"&before_id=200")
	if w.Code != 200 || len(second.Items) != 1 || second.Items[0].ID != 100 || second.NextBeforeID != 0 || second.Items[0].SiteName != "Original name" {
		t.Fatalf("new insertion shifted the next page or names were lost: %s", w.Body.String())
	}
	w, scheduled := requestCheckinLogs(t, "?status=failed&source=scheduled")
	if w.Code != 200 || len(scheduled.Items) != 1 || scheduled.Items[0].ID != 300 {
		t.Fatalf("status/source filter was ignored: %s", w.Body.String())
	}
	w, batch := requestCheckinLogs(t, "?batch_id=778")
	if w.Code != 200 || len(batch.Items) != 1 || batch.Items[0].ID != 100 {
		t.Fatalf("batch filter was ignored: %s", w.Body.String())
	}
	w, empty := requestCheckinLogs(t, "?account_id=999")
	if w.Code != 200 || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty result must be an array: %s", w.Body.String())
	}
}

func TestCheckinLogQueryRejectsInvalidFilters(t *testing.T) {
	for _, query := range []string{
		"?limit=0", "?limit=101", "?limit=-1", "?site_id=-1", "?account_id=oops", "?before_id=-1",
		"?before_id=9223372036854775808", "?status=running", "?source=unknown", "?from=not-a-date",
		"?until=not-a-date", "?batch_id=-1", "?batch_id=9223372036854775808", "?from=2026-09-28T00:00:00Z&until=2026-09-27T00:00:00Z",
	} {
		t.Run(query, func(t *testing.T) {
			w, _ := requestCheckinLogs(t, query)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid query accepted: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
