package op

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
)

// Backups are complete check-in snapshots. Validate the graph before starting a
// transaction so a truncated or hand-edited dump cannot create orphan history.
func validateCheckinGraph(d *model.CheckinDump) error {
	fail := func() error { return fmt.Errorf("invalid or inconsistent checkin backup references") }
	sites := map[string]model.CheckinSite{}
	accounts := map[string]model.CheckinAccount{}
	runs := map[string]model.CheckinRun{}
	jobs := map[string]model.CheckinJob{}
	ids := map[string]bool{}
	unique := func(table, id string) bool {
		key := table + ":" + id
		if ids[key] {
			return false
		}
		ids[key] = true
		return true
	}
	validDate := func(date string) bool { _, err := time.Parse("2006-01-02", date); return err == nil }
	for _, site := range d.Sites {
		if !unique("site", site.ID) || site.Revision < 1 {
			return fail()
		}
		if site.Support != "" && site.Support != "unknown" && site.Support != "supported" && site.Support != "unsupported" {
			return fail()
		}
		sites[site.ID] = site
	}
	for _, backup := range d.Accounts {
		a := backup.CheckinAccount
		site, ok := sites[a.SiteID]
		if !unique("account", a.ID) || !ok || (site.DeletedAt != nil && a.DeletedAt == nil) {
			return fail()
		}
		if a.LastSuccessDate != "" && !validDate(a.LastSuccessDate) {
			return fail()
		}
		accounts[a.ID] = a
	}
	days := map[string]bool{}
	for _, run := range d.Runs {
		a, ok := accounts[run.AccountID]
		key := run.AccountID + ":" + run.CheckinDate
		if !unique("run", run.ID) || !ok || a.SiteID != run.SiteID || days[key] {
			return fail()
		}
		days[key] = true
		if _, err := time.LoadLocation(run.Timezone); err != nil {
			return fail()
		}
		if len(run.SiteName) > 128 || len(run.AccountName) > 128 || len(run.Fingerprint) != 64 || strings.Trim(run.Fingerprint, "0123456789abcdef") != "" || (run.ProxyFingerprint != "" && (len(run.ProxyFingerprint) != 64 || strings.Trim(run.ProxyFingerprint, "0123456789abcdef") != "")) {
			return fail()
		}
		if run.RewardMicros != nil && (run.Status != "success" || run.RewardState != "known" || run.RewardUnit == "") {
			return fail()
		}
		if len(run.RewardUnit) > 32 {
			return fail()
		}
		switch run.Phase {
		case "precheck", "submit_intent", "confirm", "finished":
		default:
			return fail()
		}
		switch run.Source {
		case "manual", "scheduled", "confirmation", "confirmation_auto":
		default:
			return fail()
		}
		runs[run.ID] = run
	}
	for _, a := range accounts {
		if a.LastRunID != "" {
			run, ok := runs[a.LastRunID]
			if !ok || run.AccountID != a.ID {
				return fail()
			}
		}
	}
	for _, job := range d.Jobs {
		if !unique("job", job.ID) || job.Total < 0 || job.Total > 1000 {
			return fail()
		}
		switch job.Status {
		case "queued", "running", "completed", "completed_with_errors", "canceled", "interrupted":
		default:
			return fail()
		}
		switch job.Source {
		case "manual", "scheduled", "confirmation", "confirmation_auto":
		default:
			return fail()
		}
		scope := job.Scope
		if scope.Kind == "confirmation" {
			if len(scope.IDs) != 1 {
				return fail()
			}
			if _, ok := runs[scope.IDs[0]]; !ok {
				return fail()
			}
		} else if err := NormalizeCheckinScope(&scope); err != nil {
			return fail()
		}
		jobs[job.ID] = job
	}
	members := map[string]bool{}
	counts := map[string]int{}
	for _, item := range d.Items {
		_, jobOK := jobs[item.JobID]
		a, accountOK := accounts[item.AccountID]
		key := item.JobID + ":" + item.AccountID + ":" + item.CheckinDate
		if !unique("item", item.ID) || !jobOK || !accountOK || a.SiteID != item.SiteID || !validDate(item.CheckinDate) || members[key] {
			return fail()
		}
		members[key] = true
		counts[item.JobID]++
		if item.RunID != "" {
			run, ok := runs[item.RunID]
			if !ok || run.AccountID != item.AccountID || run.CheckinDate != item.CheckinDate {
				return fail()
			}
		} else if item.ConfirmOnly {
			return fail()
		}
		switch item.State {
		case "queued", "running", "done":
		default:
			return fail()
		}
		if item.State == "done" {
			switch item.Outcome {
			case "success", "already_checked_in", "pending", "manual_required", "failed", "skipped":
			default:
				return fail()
			}
		}
	}
	for _, job := range jobs {
		if job.Total != counts[job.ID] {
			return fail()
		}
	}
	for _, attempt := range d.Attempts {
		run, ok := runs[attempt.RunID]
		if !unique("attempt", attempt.ID) || !ok || !members[attempt.JobID+":"+run.AccountID+":"+run.CheckinDate] {
			return fail()
		}
		if attempt.HTTPStatus < 0 || attempt.HTTPStatus > 599 || attempt.DurationMS < 0 {
			return fail()
		}
		switch attempt.Stage {
		case "precheck", "submit", "confirm", "refresh", "authenticate", "turnstile_discover", "turnstile_cloudflare_solver", "turnstile_solverq":
		default:
			return fail()
		}
		switch attempt.Reason {
		case "response_received":
		default:
			if !model.IsTurnstileReason(attempt.Reason) && !model.IsCheckinNetworkReason(attempt.Reason) {
				return fail()
			}
		}
	}
	return nil
}

func sameCheckinScope(a, b model.CheckinScope) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func validateCheckinObservation(balance *model.CheckinBalanceSnapshot, inspection *model.CheckinInspection) error {
	fail := func() error { return fmt.Errorf("invalid checkin account observation") }
	if balance != nil {
		if balance.UpdatedAt.IsZero() || (balance.Unit != "quota" && balance.Unit != "USD") {
			return fail()
		}
		for _, amount := range []struct {
			micros *int64
			state  string
		}{{balance.AmountMicros, balance.AmountState}, {balance.UsedMicros, balance.UsedState}} {
			if !slices.Contains([]string{"known", "unknown", "invalid"}, amount.state) || (amount.state == "known") != (amount.micros != nil) {
				return fail()
			}
		}
		if balance.UsedMicros != nil && *balance.UsedMicros < 0 {
			return fail()
		}
	}
	if inspection == nil {
		return nil
	}
	if inspection.Steps == nil {
		inspection.Steps = []model.CheckinInspectionStep{}
	}
	if _, err := uuid.Parse(inspection.ID); err != nil {
		return fail()
	}
	if inspection.Kind != "balance" && inspection.Kind != "detect" {
		return fail()
	}
	if !slices.Contains([]string{"running", "success", "partial", "failed", "unsupported", "interrupted"}, inspection.Status) ||
		!slices.Contains([]string{"valid", "invalid", "unknown"}, inspection.Authentication) ||
		!slices.Contains([]string{"not_checked", "unknown", "supported", "unsupported", "disabled"}, inspection.CheckinSupport) ||
		!slices.Contains([]string{"unknown", "checked_in", "not_checked_in"}, inspection.CheckinStatus) {
		return fail()
	}
	if _, err := time.Parse("2006-01-02", inspection.CheckinDate); err != nil {
		return fail()
	}
	if _, err := time.LoadLocation(inspection.Timezone); err != nil {
		return fail()
	}
	if inspection.UserID != "" {
		id, err := strconv.ParseInt(inspection.UserID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != inspection.UserID {
			return fail()
		}
	}
	if inspection.Authentication == "valid" && inspection.UserID == "" {
		return fail()
	}
	if inspection.StartedAt.IsZero() || (inspection.Status == "running") != (inspection.FinishedAt == nil) ||
		(inspection.Status == "running") != (inspection.Reason == "") ||
		(inspection.FinishedAt != nil && inspection.FinishedAt.Before(inspection.StartedAt)) ||
		(inspection.BalanceUpdated && balance == nil) || len(inspection.Steps) > 32 {
		return fail()
	}
	switch inspection.Reason {
	case "", "account_verified", "balance_unknown", "balance_invalid", "inspection_unsupported", "inspection_interrupted", "inspection_failed", "account_response_invalid", "account_identity_mismatch", "account_unavailable", "checkin_status_unknown", "checkin_disabled", "credential_unavailable", "invalid_credentials", "invalid_proxy", "network_error", "rate_limited", "upstream_http_error", "verification_required", "unsupported_endpoint", "config_changed", "sub2api_refresh_required", "sub2api_refresh_deferred", "sub2api_refresh_uncertain", "sub2api_refresh_rejected", "sub2api_auth_rejected", "sub2api_session_revoked", "sub2api_session_binding_mismatch", "sub2api_account_unavailable", "sub2api_refresh_identity_mismatch", "sub2api_refresh_save_failed":
	default:
		if !model.IsTurnstileReason(inspection.Reason) {
			return fail()
		}
	}
	for _, step := range inspection.Steps {
		if !slices.Contains([]string{"account", "refresh", "detect_status", "turnstile_discover", "turnstile_cloudflare_solver", "turnstile_solverq"}, step.Stage) || (step.Stage == "detect_status" && inspection.Kind != "detect") ||
			(!slices.Contains([]string{"response_received"}, step.Reason) && !model.IsTurnstileReason(step.Reason) && !model.IsCheckinNetworkReason(step.Reason)) || step.HTTPStatus < 0 || step.HTTPStatus > 599 || step.DurationMS < 0 || step.CreatedAt.IsZero() {
			return fail()
		}
	}
	return nil
}
