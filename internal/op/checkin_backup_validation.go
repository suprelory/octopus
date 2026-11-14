package op

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
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
		if len(run.SiteName) > 128 || len(run.AccountName) > 128 || len(run.Fingerprint) != 64 || strings.Trim(run.Fingerprint, "0123456789abcdef") != "" {
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
		case "precheck", "submit", "confirm":
		default:
			return fail()
		}
		switch attempt.Reason {
		case "response_received", "network_error":
		default:
			return fail()
		}
	}
	return nil
}

func sameCheckinScope(a, b model.CheckinScope) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
