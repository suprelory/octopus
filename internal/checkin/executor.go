package checkin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) execute(ctx context.Context, item model.CheckinJobItem, settings model.CheckinSettings) error {
	complete := func(run *model.CheckinRun, outcome string, next *time.Time) error {
		// A canceled browser/worker must not discard a response already received.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return s.Store.Complete(saveCtx, item, run, outcome, next)
	}
	account, err := s.Store.Account(ctx, item.AccountID)
	if err != nil {
		return complete(nil, "skipped", nil)
	}
	site, err := s.Store.Site(ctx, account.SiteID)
	if err != nil || !site.Config.Enabled || !account.Enabled {
		return complete(nil, "skipped", nil)
	}
	var job model.CheckinJob
	if err := s.Store.DB.WithContext(ctx).First(&job, "id = ?", item.JobID).Error; err != nil {
		return err
	}
	if job.Status == "canceled" {
		return complete(nil, "skipped", nil)
	}
	if job.Source == "scheduled" && (!account.AutoCheckin || !inWindow(site.Config, s.Now())) {
		return complete(nil, "skipped", nil)
	}
	run, err := s.Store.BeginRun(ctx, item, site, account, fingerprint(site, account), job.Source, s.Now())
	if err != nil {
		return err
	}
	item.RunID = run.ID
	if run.Status == "success" || run.Status == "already_checked_in" {
		return complete(nil, "already_checked_in", nil)
	}
	finish := func(status, reason string) error {
		run.Status, run.Reason, run.Phase = status, reason, "finished"
		run.NextConfirmAt = nil
		if status == "pending" {
			if run.ConfirmDeadline != nil && !s.Now().Before(*run.ConfirmDeadline) {
				run.Status, run.Reason = "manual_required", "confirmation_expired"
			} else {
				next := s.Now().Add(time.Minute)
				run.NextConfirmAt = &next
				run.Phase = "confirm"
				if run.SubmitIntentAt == nil {
					run.Phase = "precheck"
				}
			}
		}
		var next *time.Time
		if account.AutoCheckin {
			if run.Status == "success" || run.Status == "already_checked_in" {
				if account.LastSuccessDate < run.CheckinDate {
					account.LastSuccessDate = run.CheckinDate
				}
				value := nextSchedule(site.Config, account, s.Now(), nil)
				next = &value
			} else if run.Status == "failed" {
				value := s.Now().Add(15 * time.Minute)
				if !inWindow(site.Config, value) {
					value = nextSchedule(site.Config, account, value, nil)
				}
				next = &value
			}
		}
		if run.Status == "success" || run.Status == "already_checked_in" || run.Reason == "unsupported_endpoint" {
			support := "supported"
			if run.Reason == "unsupported_endpoint" {
				support = "unsupported"
			}
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			err := s.Store.RecordSupport(saveCtx, site, support, s.Now())
			cancel()
			if err != nil {
				return err
			}
		}
		return complete(&run, run.Status, next)
	}
	authFailure := func(err error) error {
		if status, reason, ok := adapterErrorState(err); ok {
			return finish(status, reason)
		}
		return err
	}
	currentFingerprint := fingerprint(site, account)
	resolvedProxy, err := op.ResolveProxy(ctx, s.Store.DB, account.ProxySelection(site.Config))
	if err != nil {
		return finish("failed", "invalid_proxy")
	}
	if run.Fingerprint != currentFingerprint || (run.ProxyFingerprint != "" && run.ProxyFingerprint != resolvedProxy.Fingerprint) {
		if run.SubmitIntentAt == nil || run.SafeToRetry {
			run.Fingerprint = currentFingerprint
		} else if item.ConfirmOnly && job.Source == "confirmation" {
			// Explicit rechecks may use repaired configuration, but cannot claim a
			// new reward or submit under that changed identity/configuration.
			run.Fingerprint, run.BeforeUnchecked = currentFingerprint, false
		} else {
			return finish("manual_required", "config_changed")
		}
	}
	run.ProxyFingerprint = resolvedProxy.Fingerprint
	if err := s.Store.SaveRunProxy(ctx, item, run, s.Now()); err != nil {
		return err
	}
	if run.ConfirmDeadline == nil || (run.SubmitIntentAt == nil && (run.Status == "failed" || job.Source == "manual")) {
		deadline := s.Now().Add(time.Duration(settings.ConfirmTimeoutMinutes) * time.Minute)
		run.ConfirmDeadline = &deadline
	}
	if (job.Source == "confirmation_auto" || job.Source == "scheduled") && !s.Now().Before(*run.ConfirmDeadline) {
		return finish("manual_required", "confirmation_expired")
	}
	plain, err := s.Vault.Unseal(account.ID, account.CredentialCipher)
	if err != nil {
		return finish("manual_required", "credential_unavailable")
	}
	var credentials model.CheckinCredentials
	if json.Unmarshal(plain, &credentials) != nil || model.ValidateCheckinCredentials(credentials) != nil || !model.CheckinCredentialSupported(site.Config.Adapter, credentials.Type) {
		return finish("manual_required", "invalid_credentials")
	}
	client, err := newHTTPClient(resolvedProxy.URL, time.Duration(settings.HTTPTimeoutSeconds)*time.Second)
	if err != nil {
		return finish("failed", "invalid_proxy")
	}
	defer client.CloseIdleConnections()
	adapter, err := newPlatformAdapter(&adapterRuntime{s: s, client: client, site: site, item: item, account: &account, credentials: &credentials, run: &run, source: job.Source})
	if err != nil {
		return authFailure(err)
	}
	policy := adapter.Policy()
	var submitBody []byte
	var submittedResult *SubmitResult
	shouldSubmit := !item.ConfirmOnly && (run.SubmitIntentAt == nil || run.SafeToRetry)
	if shouldSubmit && businessDate(site.Config, s.Now()) != run.CheckinDate {
		return finish("skipped", "date_expired")
	}
	if err := adapter.Prepare(ctx, !shouldSubmit); err != nil {
		return authFailure(err)
	}
	if shouldSubmit {
		if policy.Available {
			result, err := adapter.Query(ctx, "precheck")
			if err != nil {
				return authFailure(err)
			}
			if result.State == "checked_in" {
				return finish("already_checked_in", "already_checked_in")
			}
			if policy.RequiredBeforeSubmit {
				if result.HTTP.StatusCode == 401 || result.HTTP.StatusCode == 403 {
					return finish("manual_required", "invalid_credentials")
				}
				if result.HTTP.StatusCode == 404 || result.HTTP.StatusCode == 405 {
					return finish("failed", "unsupported_endpoint")
				}
				if result.State != "not_checked_in" {
					return finish("pending", "precheck_unknown")
				}
			}
			run.BeforeUnchecked = result.State == "not_checked_in"
		}
		if ctx.Err() != nil {
			return finish("pending", "interrupted_before_submit")
		}
		if businessDate(site.Config, s.Now()) != run.CheckinDate || (job.Source == "scheduled" && !inWindow(site.Config, s.Now())) {
			return finish("skipped", "date_expired")
		}
		if err := s.Store.MarkSubmitted(ctx, item, &run, s.Now()); err != nil {
			return err
		}
		submitted, err := adapter.Submit(ctx)
		if err != nil {
			return authFailure(err)
		}
		submitBody = submitted.HTTP.Body
		result := submitted.Result
		submittedResult = &result
		if !policy.Available || submitted.Final {
			run.SafeToRetry = result.SafeToRetry
			if result.Status == "success" {
				run.RewardMicros, run.RewardState = adapter.Reward(submitBody, nil)
				run.RewardUnit = site.Config.Reward.Unit
			}
			if result.Reason == "invalid_credentials" {
				result.Status = "manual_required"
			}
			return finish(result.Status, result.Reason)
		}
		if result.Status == "already_checked_in" {
			run.BeforeUnchecked = false
		}
	}
	if !policy.Available {
		return finish("manual_required", "status_query_required")
	}
	if businessDate(site.Config, s.Now()) != run.CheckinDate && !policy.DateAware {
		return finish("manual_required", "date_evidence_required")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < settings.ConfirmAttempts; attempt++ {
		if attempt > 0 && !waitContext(queryCtx, time.Duration(settings.ConfirmIntervalSeconds)*time.Second) {
			break
		}
		result, err := adapter.Query(queryCtx, "confirm")
		if err != nil {
			return authFailure(err)
		}
		if result.State == "checked_in" {
			newCheckin := run.SubmitIntentAt != nil && run.BeforeUnchecked
			if result.CheckedAt != nil && run.SubmitIntentAt != nil {
				newCheckin = result.CheckedAt.Unix() >= run.SubmitIntentAt.Unix()
			}
			if newCheckin {
				run.RewardMicros, run.RewardState = adapter.Reward(submitBody, result.RewardBody)
				run.RewardUnit = site.Config.Reward.Unit
				return finish("success", "checked_in")
			}
			return finish("already_checked_in", "already_checked_in")
		}
		if result.HTTP.StatusCode == 401 || result.HTTP.StatusCode == 403 {
			return finish("manual_required", "invalid_credentials")
		}
		if queryCtx.Err() != nil {
			break
		}
	}
	if submittedResult != nil && submittedResult.SafeToRetry {
		run.SafeToRetry = true
		return finish(submittedResult.Status, submittedResult.Reason)
	}
	return finish("pending", "pending_confirmation")
}
