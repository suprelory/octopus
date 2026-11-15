package checkin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

type agentRouterAdapter struct {
	r       *adapterRuntime
	session *routerSession
}

func newAgentRouterAdapter(r *adapterRuntime) platformAdapter { return &agentRouterAdapter{r: r} }
func (a *agentRouterAdapter) Policy() statusPolicy {
	return statusPolicy{Available: true, DateAware: true}
}
func (a *agentRouterAdapter) Prepare(ctx context.Context, _ bool) error {
	var err error
	a.session, err = openRouterSession(ctx, a.r)
	return err // Never log in here: login itself is the check-in mutation.
}
func (a *agentRouterAdapter) Reward([]byte, []byte) (*int64, string) { return nil, "unknown" }

func (a *agentRouterAdapter) Submit(ctx context.Context) (submission, error) {
	response, data, err := a.session.login(ctx, "submit")
	result := submission{HTTP: response, Result: SubmitResult{Status: "manual_required", Reason: "router_login_unconfirmed"}}
	if err != nil {
		// Regardless of HTTP status, a failed/uncertain login is never replayed.
		return result, err
	}
	checked, _ := field(data, "data.checked_in")
	date, hasDate := field(data, "data.checkin_date")
	confirmed := checked == true && ((hasDate && date == a.r.run.CheckinDate) || (!hasDate && businessDate(a.r.site.Config, a.r.s.Now()) == a.r.run.CheckinDate))
	if !confirmed {
		return result, nil
	}
	// Earlier logs distinguish an already granted allowance. Missing logs cannot
	// undo explicit, same-day checked_in evidence in this login response.
	queryCtx, cancel := context.WithTimeout(ctx, a.r.turnstileQueryTimeout(5*time.Second))
	defer cancel()
	status, err := a.Query(queryCtx, "confirm")
	if err != nil {
		if _, _, recognized := adapterErrorState(err); !recognized {
			return result, err
		}
	}
	result.Final = true
	result.Result = SubmitResult{Status: "success", Reason: "checked_in"}
	if status.CheckedAt != nil && a.r.run.SubmitIntentAt != nil && status.CheckedAt.Unix() < a.r.run.SubmitIntentAt.Unix() {
		result.Result = SubmitResult{Status: "already_checked_in", Reason: "already_checked_in"}
	}
	return result, nil
}

func (a *agentRouterAdapter) Query(ctx context.Context, stage string) (statusResult, error) {
	result := statusResult{State: "unknown"}
	if !a.session.hasAuth("/api/log/self/") || a.r.credentials.UserID == "" {
		if stage == "precheck" {
			return result, nil
		}
		return result, needsAttention("router_session_required")
	}
	location, err := time.LoadLocation(a.r.run.Timezone)
	if err != nil {
		return result, err
	}
	start, err := time.ParseInLocation("2006-01-02", a.r.run.CheckinDate, location)
	if err != nil {
		return result, err
	}
	end := start.AddDate(0, 0, 1).Add(-time.Second)
	if now := a.r.s.Now(); end.After(now) {
		end = now
	}
	if end.Before(start) {
		return result, nil
	}
	path := fmt.Sprintf("/api/log/self/?p=1&page_size=20&type=4&start_timestamp=%d&end_timestamp=%d", start.Unix(), end.Unix())
	response, err := a.session.request(ctx, stage, model.CheckinRequest{Method: "GET", Path: path}, false)
	if err != nil {
		return result, err
	}
	if err := a.session.persist(ctx); err != nil {
		return result, err
	}
	result.HTTP = response
	result.CheckedAt = agentRouterLogTime(response, start, end)
	if result.CheckedAt != nil {
		result.State = "checked_in"
	}
	// An empty/truncated log page does not prove that no check-in happened.
	return result, nil
}

func agentRouterLogTime(response HTTPResult, start, end time.Time) *time.Time {
	data, err := decodeObject(response.Body)
	if err != nil || !routerSuccessResponse(response) || !matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("true")}) {
		return nil
	}
	var items []any
	for _, path := range []string{"data.items", "items", "data"} {
		value, _ := field(data, path)
		if array, ok := value.([]any); ok {
			items = array
			break
		}
	}
	var earliest *time.Time
	for _, item := range items {
		if !matches(item, model.CheckinMatch{Path: "type", Value: json.RawMessage("4")}) {
			continue
		}
		value, _ := field(item, "content")
		content, _ := value.(string)
		content = strings.ToLower(content)
		if !strings.Contains(content, "签到成功") && !strings.Contains(content, "簽到成功") && !strings.Contains(content, "daily check-in success") && !strings.Contains(content, "daily checkin success") {
			continue
		}
		value, _ = field(item, "created_at")
		number, ok := value.(json.Number)
		if !ok {
			continue
		}
		stamp, err := number.Int64()
		if err != nil || stamp < start.Unix() || stamp > end.Unix() {
			continue
		}
		at := time.Unix(stamp, 0)
		if earliest == nil || at.Before(*earliest) {
			earliest = &at
		}
	}
	return earliest
}
