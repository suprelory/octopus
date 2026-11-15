package checkin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// New API exposes monthly records at GET /api/user/checkin?month=YYYY-MM.
// checked_in_today alone is insufficient evidence for a previous day's run.
type newAPIAdapter struct{ requestAdapter }

func (a *newAPIAdapter) Policy() statusPolicy {
	if model.CheckinNativeStatus(a.r.site.Config) {
		return statusPolicy{Available: true, RequiredBeforeSubmit: true, DateAware: true}
	}
	return a.requestAdapter.Policy()
}

func (a *newAPIAdapter) Query(ctx context.Context, stage string) (statusResult, error) {
	if !model.CheckinNativeStatus(a.r.site.Config) {
		return a.requestAdapter.Query(ctx, stage)
	}
	date := a.r.run.CheckinDate
	response, err := a.r.request(ctx, stage, model.CheckinRequest{Method: "GET", Path: "/api/user/checkin?month=" + date[:7]}, *a.r.credentials)
	result := newAPIStatus(response, date, businessDate(a.r.site.Config, a.r.s.Now()))
	return result, err
}

func newAPIStatus(response HTTPResult, date, today string) statusResult {
	result := statusResult{HTTP: response, State: "unknown"}
	if response.Err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return result
	}
	data, err := decodeObject(response.Body)
	if err != nil || !matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("true")}) || !matches(data, model.CheckinMatch{Path: "data.enabled", Value: json.RawMessage("true")}) {
		return result
	}
	value, _ := field(data, "data.stats.records")
	records, ok := value.([]any)
	if !ok {
		return result
	}
	var found map[string]any
	for _, entry := range records {
		record, ok := entry.(map[string]any)
		if !ok {
			return result
		}
		day, ok := record["checkin_date"].(string)
		if _, err := time.Parse("2006-01-02", day); !ok || err != nil {
			return result
		}
		if day == date {
			if found != nil {
				return result
			}
			found = record
		}
	}
	todayValue, _ := field(data, "data.stats.checked_in_today")
	checkedToday, validToday := todayValue.(bool)
	if found != nil {
		if date == today && validToday && !checkedToday {
			return result
		}
		result.State = "checked_in"
		result.RewardBody, _ = json.Marshal(map[string]any{"success": true, "data": found})
	} else if date == today && validToday && !checkedToday {
		result.State = "not_checked_in"
	}
	return result
}
