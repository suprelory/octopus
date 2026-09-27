package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func TestManualFullCheckinReturnsAndQueriesBatchTask(t *testing.T) {
	ctx := setupSiteHandlerTestDB(t)
	w, response := requestSiteMutation(t, "/api/v1/site/checkin-all", checkinAllSiteAccounts, map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("start batch returned %d: %s", w.Code, w.Body.String())
	}
	data, ok := response.Data.(map[string]any)
	if !ok || data["id"] == "" || data["status"] == "" {
		t.Fatalf("task id and status were not returned: %s", w.Body.String())
	}
	taskID, err := strconv.ParseInt(data["id"].(string), 10, 64)
	if err != nil || taskID <= 0 {
		t.Fatalf("invalid task id: %#v", data["id"])
	}

	deadline := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	for {
		job, err := querySiteCheckinBatch(t, strconv.FormatInt(taskID, 10), getSiteCheckinBatch)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != model.SiteCheckinBatchJobStatusQueued && job.Status != model.SiteCheckinBatchJobStatusRunning {
			if job.Status != model.SiteCheckinBatchJobStatusCompleted || job.Total != 0 {
				t.Fatalf("unexpected empty task result: %+v", job)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("task query timed out")
		case <-deadline.C:
		}
	}
	latest, err := querySiteCheckinBatch(t, "", getLatestSiteCheckinBatch)
	if err != nil || latest.ID != taskID {
		t.Fatalf("latest task query returned %+v, %v", latest, err)
	}
}

func querySiteCheckinBatch(t *testing.T, taskID string, handler gin.HandlerFunc) (*model.SiteCheckinBatchJob, error) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	path := "/api/v1/site/checkin-batches/latest"
	if taskID != "" {
		path = "/api/v1/site/checkin-batches/" + taskID
		c.Params = gin.Params{{Key: "id", Value: taskID}}
	}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	handler(c)
	if w.Code != http.StatusOK {
		return nil, strconv.ErrSyntax
	}
	var response struct {
		Data model.SiteCheckinBatchJob `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}
