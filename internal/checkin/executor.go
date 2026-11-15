package checkin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bestruirui/octopus/internal/model"
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
		if status, reason, ok := sub2APIErrorState(err); ok {
			return finish(status, reason)
		}
		return err
	}
	currentFingerprint := fingerprint(site, account)
	if run.Fingerprint != currentFingerprint {
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
	if json.Unmarshal(plain, &credentials) != nil || model.ValidateCheckinCredentials(credentials) != nil {
		return finish("manual_required", "invalid_credentials")
	}
	proxy := site.Config.ProxyURL
	if account.ProxyMode == "direct" {
		proxy = ""
	}
	if account.ProxyMode == "custom" {
		proxy = account.ProxyURL
	}
	client, err := newHTTPClient(proxy, time.Duration(settings.HTTPTimeoutSeconds)*time.Second)
	if err != nil {
		return finish("failed", "invalid_proxy")
	}
	defer client.CloseIdleConnections()
	sub2api := site.Config.Adapter == "sub2api" && credentials.Type == "bearer"
	refreshed := false
	if sub2api {
		refreshed, err = s.ensureSub2APISession(ctx, client, site, item, &account, &credentials, false)
		if err != nil {
			return authFailure(err)
		}
	}
	request := func(requestCtx context.Context, stage string, config model.CheckinRequest) (HTTPResult, error) {
		started := time.Now()
		result := requestHTTP(requestCtx, client, site.Config, config, credentials, run.CheckinDate)
		reason, message := "response_received", "Upstream response received"
		if result.Err != nil {
			reason, message = "network_error", "Request failed, timed out, or exceeded the response limit"
		}
		// Never persist upstream bodies, headers, URLs or credential-derived text.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err := s.Store.AddAttempt(saveCtx, model.CheckinAttempt{RunID: run.ID, JobID: item.JobID, Stage: stage, HTTPStatus: result.StatusCode, Reason: reason, Message: message, DurationMS: time.Since(started).Milliseconds(), CreatedAt: s.Now()})
		return result, err
	}
	var submitBody []byte
	var submittedResult *SubmitResult
	statusQuery := func(queryCtx context.Context, stage string) (HTTPResult, string, error) {
		if site.Config.Status == nil {
			return HTTPResult{}, "unknown", nil
		}
		if businessDate(site.Config, s.Now()) != run.CheckinDate && site.Config.Status.DatePath == "" {
			return HTTPResult{}, "unknown", nil
		}
		response, err := request(queryCtx, stage, site.Config.Status.Request)
		if err != nil {
			return response, "unknown", err
		}
		if sub2api && sub2APIRecoverable(response) && !refreshed {
			refreshed, err = s.ensureSub2APISession(queryCtx, client, site, item, &account, &credentials, true)
			if err != nil {
				return response, "unknown", err
			}
			response, err = request(queryCtx, stage, site.Config.Status.Request)
			if err != nil {
				return response, "unknown", err
			}
		}
		if sub2api && response.Err == nil && (response.StatusCode == 401 || response.StatusCode == 403) {
			return response, "unknown", sub2APIRejected(response)
		}
		if response.Err != nil {
			return response, "unknown", nil
		}
		return response, evaluateSiteStatus(site.Config, response.StatusCode, response.Body, run.CheckinDate), nil
	}
	shouldSubmit := !item.ConfirmOnly && (run.SubmitIntentAt == nil || run.SafeToRetry)
	if shouldSubmit {
		if businessDate(site.Config, s.Now()) != run.CheckinDate {
			return finish("skipped", "date_expired")
		}
		if site.Config.Status != nil {
			response, state, err := statusQuery(ctx, "precheck")
			if err != nil {
				return authFailure(err)
			}
			if state == "checked_in" {
				return finish("already_checked_in", "already_checked_in")
			}
			if response.StatusCode == 401 || response.StatusCode == 403 {
				return finish("manual_required", "invalid_credentials")
			}
			if response.StatusCode == 404 || response.StatusCode == 405 {
				return finish("failed", "unsupported_endpoint")
			}
			if state != "not_checked_in" {
				return finish("pending", "precheck_unknown")
			}
			run.BeforeUnchecked = true
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
		response, err := request(ctx, "submit", site.Config.Submit)
		if err != nil {
			return err
		}
		submitBody = response.Body
		result := SubmitResult{Status: "manual_required", Reason: "network_error"}
		if response.Err == nil {
			result = interpretSubmit(site.Config, response.StatusCode, response.Body)
		}
		if sub2api && response.Err == nil && (response.StatusCode == 401 || response.StatusCode == 403) {
			// Authentication recovery never clears the durable submit marker,
			// even when the configured submission method is GET.
			result.SafeToRetry = false
			if !sub2APIRecoverable(response) {
				return authFailure(sub2APIRejected(response))
			}
			if !refreshed {
				refreshed, err = s.ensureSub2APISession(ctx, client, site, item, &account, &credentials, true)
				if err != nil {
					return authFailure(err)
				}
			}
			if site.Config.Status == nil {
				return finish("manual_required", "sub2api_submission_unconfirmed")
			}
		}
		submittedResult = &result
		if site.Config.Status == nil {
			run.SafeToRetry = result.SafeToRetry
			if result.Status == "success" {
				run.RewardMicros, run.RewardState = extractReward(site.Config.Reward, submitBody)
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
	if site.Config.Status == nil {
		return finish("manual_required", "status_query_required")
	}
	if businessDate(site.Config, s.Now()) != run.CheckinDate && site.Config.Status.DatePath == "" {
		return finish("manual_required", "date_evidence_required")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < settings.ConfirmAttempts; attempt++ {
		if attempt > 0 && !waitContext(queryCtx, time.Duration(settings.ConfirmIntervalSeconds)*time.Second) {
			break
		}
		response, state, err := statusQuery(queryCtx, "confirm")
		if err != nil {
			return authFailure(err)
		}
		if state == "checked_in" {
			if run.SubmitIntentAt != nil && run.BeforeUnchecked {
				body := submitBody
				if site.Config.Reward.Source == "status" {
					body = response.Body
				}
				run.RewardMicros, run.RewardState = extractReward(site.Config.Reward, body)
				run.RewardUnit = site.Config.Reward.Unit
				return finish("success", "checked_in")
			}
			return finish("already_checked_in", "already_checked_in")
		}
		if response.StatusCode == 401 || response.StatusCode == 403 {
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
