package checkin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

type statusPolicy struct {
	Available, RequiredBeforeSubmit, DateAware bool
}

type statusResult struct {
	HTTP       HTTPResult
	State      string
	RewardBody []byte
	CheckedAt  *time.Time
}

type submission struct {
	HTTP   HTTPResult
	Result SubmitResult
	// Explicit platform evidence can complete a submission without a separate
	// endpoint (AgentRouter's checked_in flag). Generic success flags cannot.
	Final bool
}

// Submit is the only method allowed to perform a check-in mutation. The executor
// persists the intent before calling it. Prepare(readOnly=true) and Query must
// never perform that mutation, including login on login-as-check-in platforms.
type platformAdapter interface {
	Prepare(ctx context.Context, readOnly bool) error
	Policy() statusPolicy
	Query(ctx context.Context, stage string) (statusResult, error)
	Submit(ctx context.Context) (submission, error)
	Reward(submitBody, statusBody []byte) (*int64, string)
}

type adapterRuntime struct {
	s           *Service
	client      *http.Client
	site        model.CheckinSite
	item        model.CheckinJobItem
	account     *model.CheckinAccount
	credentials *model.CheckinCredentials
	run         *model.CheckinRun
	inspection  *model.CheckinInspection
	source      string
	turnstile   *turnstileSession
}

func (r *adapterRuntime) request(ctx context.Context, stage string, config model.CheckinRequest, auth model.CheckinCredentials) (HTTPResult, error) {
	return r.requestBody(ctx, stage, config, auth, true)
}

func (r *adapterRuntime) requestBody(ctx context.Context, stage string, config model.CheckinRequest, auth model.CheckinCredentials, expand bool) (HTTPResult, error) {
	if err := r.initTurnstile(ctx); err != nil {
		return HTTPResult{}, err
	}
	started := time.Now()
	date := r.item.CheckinDate
	if r.run != nil {
		date = r.run.CheckinDate
	}
	result := doRequestHTTP(ctx, r.client, r.site.Config, config, auth, date, expand)
	if r.turnstileChallenge(result) {
		if err := r.markTurnstileRejection(ctx, stage); err != nil {
			return result, err
		}
		if err := r.auditReason(ctx, stage, result, time.Since(started), "turnstile_required"); err != nil {
			return result, err
		}
		if err := r.turnstileRetryAllowed(ctx, stage == "submit"); err != nil {
			return result, err
		}
		token, err := r.solveTurnstile(ctx, result)
		if err != nil {
			return result, err
		}
		if err := r.turnstileRetryAllowed(ctx, stage == "submit"); err != nil {
			return result, err
		}
		if stage == "submit" && r.run != nil {
			if err := r.s.Store.MarkSubmitted(ctx, r.item, r.run, r.s.Now()); err != nil {
				return result, err
			}
		}
		token.cookies = result.Cookies
		started = time.Now()
		result = doRequestHTTPWithToken(ctx, r.client, r.site.Config, config, auth, date, expand, token)
		result.Cookies = append(token.cookies, result.Cookies...)
		if r.turnstileChallenge(result) {
			if err := r.markTurnstileRejection(ctx, stage); err != nil {
				return result, err
			}
			if err := r.auditReason(ctx, stage, result, time.Since(started), "turnstile_rejected"); err != nil {
				return result, err
			}
			return result, needsAttention("turnstile_rejected")
		}
	}
	return result, r.audit(ctx, stage, result, time.Since(started))
}

func (r *adapterRuntime) audit(ctx context.Context, stage string, result HTTPResult, duration time.Duration) error {
	reason := "response_received"
	if result.Err != nil {
		reason = result.NetworkReason
		if !model.IsCheckinNetworkReason(reason) {
			reason = model.CheckinNetworkError
		}
	}
	return r.auditReason(ctx, stage, result, duration, reason)
}

func (r *adapterRuntime) auditReason(ctx context.Context, stage string, result HTTPResult, duration time.Duration, reason string) error {
	message := "Upstream response received"
	if model.IsCheckinNetworkReason(reason) {
		message = "Request failed, timed out, or exceeded the response limit"
	}
	if model.IsTurnstileReason(reason) {
		message = "Turnstile verification step completed"
	}
	// Never persist bodies, headers, URLs or credential-derived text.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if r.inspection != nil {
		r.inspection.Steps = append(r.inspection.Steps, model.CheckinInspectionStep{Stage: stage, HTTPStatus: result.StatusCode, Reason: reason, DurationMS: duration.Milliseconds(), CreatedAt: r.s.Now()})
		return r.s.Store.SaveInspection(saveCtx, r.item, *r.account, r.inspection, nil, false, r.s.Now())
	}
	return r.s.Store.AddAttempt(saveCtx, model.CheckinAttempt{RunID: r.run.ID, JobID: r.item.JobID, Stage: stage, HTTPStatus: result.StatusCode, Reason: reason, Message: message, DurationMS: duration.Milliseconds(), CreatedAt: r.s.Now()})
}

type adapterError struct{ status, reason string }

func (e *adapterError) Error() string    { return e.reason }
func needsAttention(reason string) error { return &adapterError{"manual_required", reason} }

func adapterErrorState(err error) (string, string, bool) {
	var failure *adapterError
	if errors.As(err, &failure) {
		return failure.status, failure.reason, true
	}
	return sub2APIErrorState(err)
}

var adapterFactories = map[string]func(*adapterRuntime) platformAdapter{
	"http":        newRequestAdapter,
	"one-api":     newRequestAdapter,
	"one-hub":     newRequestAdapter,
	"done-hub":    newRequestAdapter,
	"new-api":     func(r *adapterRuntime) platformAdapter { return &newAPIAdapter{requestAdapter{r}} },
	"sub2api":     func(r *adapterRuntime) platformAdapter { return &sub2APIAdapter{requestAdapter: requestAdapter{r}} },
	"anyrouter":   newAnyRouterAdapter,
	"agentrouter": newAgentRouterAdapter,
}

func newPlatformAdapter(r *adapterRuntime) (platformAdapter, error) {
	factory, ok := adapterFactories[r.site.Config.Adapter]
	if !ok {
		return nil, needsAttention("unsupported_platform")
	}
	return factory(r), nil
}

type requestAdapter struct{ r *adapterRuntime }

func newRequestAdapter(r *adapterRuntime) platformAdapter     { return &requestAdapter{r} }
func (a *requestAdapter) Prepare(context.Context, bool) error { return nil }
func (a *requestAdapter) Policy() statusPolicy {
	rule := a.r.site.Config.Status
	return statusPolicy{Available: rule != nil, RequiredBeforeSubmit: rule != nil, DateAware: rule != nil && rule.DatePath != ""}
}
func (a *requestAdapter) Query(ctx context.Context, stage string) (statusResult, error) {
	rule := a.r.site.Config.Status
	if rule == nil || !a.queryDateValid() {
		return statusResult{State: "unknown"}, nil
	}
	response, err := a.r.request(ctx, stage, rule.Request, *a.r.credentials)
	return a.status(response), err
}
func (a *requestAdapter) status(response HTTPResult) statusResult {
	result := statusResult{HTTP: response, State: "unknown", RewardBody: response.Body}
	if response.Err == nil && a.r.site.Config.Status != nil && a.queryDateValid() {
		result.State = evaluateSiteStatus(a.r.site.Config, response.StatusCode, response.Body, a.r.run.CheckinDate)
	}
	return result
}

func (a *requestAdapter) queryDateValid() bool {
	return a.r.site.Config.Status != nil && (a.r.site.Config.Status.DatePath != "" || businessDate(a.r.site.Config, a.r.s.Now()) == a.r.run.CheckinDate)
}
func (a *requestAdapter) Submit(ctx context.Context) (submission, error) {
	response, err := a.r.request(ctx, "submit", a.r.site.Config.Submit, *a.r.credentials)
	return submission{HTTP: response, Result: a.interpret(response)}, err
}
func (a *requestAdapter) interpret(response HTTPResult) SubmitResult {
	if response.Err != nil {
		return SubmitResult{Status: "manual_required", Reason: "network_error"}
	}
	return interpretSubmit(a.r.site.Config, response.StatusCode, response.Body)
}
func (a *requestAdapter) Reward(submitBody, statusBody []byte) (*int64, string) {
	body := submitBody
	if a.r.site.Config.Reward.Source == "status" {
		body = statusBody
	}
	return extractReward(a.r.site.Config.Reward, body)
}

type sub2APIAdapter struct {
	requestAdapter
	refreshed bool
}

func (a *sub2APIAdapter) refresh(ctx context.Context, force bool) error {
	if a.r.credentials.Type != "bearer" {
		return nil
	}
	refreshed, err := a.r.ensureSub2APISession(ctx, force)
	a.refreshed = a.refreshed || refreshed
	return err
}
func (a *sub2APIAdapter) Prepare(ctx context.Context, _ bool) error { return a.refresh(ctx, false) }
func (a *sub2APIAdapter) Query(ctx context.Context, stage string) (statusResult, error) {
	rule := a.r.site.Config.Status
	if rule == nil || !a.queryDateValid() {
		return statusResult{State: "unknown"}, nil
	}
	response, err := a.readRequest(ctx, stage, rule.Request)
	return a.status(response), err
}

// Only read operations may be replayed after one successful token rotation.
func (a *sub2APIAdapter) readRequest(ctx context.Context, stage string, request model.CheckinRequest) (HTTPResult, error) {
	result, err := a.r.request(ctx, stage, request, *a.r.credentials)
	if err != nil || a.r.credentials.Type != "bearer" {
		return result, err
	}
	if sub2APIRecoverable(result) && !a.refreshed {
		if err := a.refresh(ctx, true); err != nil {
			return result, err
		}
		result, err = a.r.request(ctx, stage, request, *a.r.credentials)
	}
	if err == nil && result.Err == nil && (result.StatusCode == 401 || result.StatusCode == 403) {
		err = sub2APIRejected(result)
	}
	return result, err
}
func (a *sub2APIAdapter) Submit(ctx context.Context) (submission, error) {
	result, err := a.requestAdapter.Submit(ctx)
	response := result.HTTP
	if err != nil || a.r.credentials.Type != "bearer" || response.Err != nil || (response.StatusCode != 401 && response.StatusCode != 403) {
		return result, err
	}
	// Authentication recovery never clears the durable submit marker.
	result.Result.SafeToRetry = false
	if !sub2APIRecoverable(response) {
		return result, sub2APIRejected(response)
	}
	if !a.refreshed {
		if err := a.refresh(ctx, true); err != nil {
			return result, err
		}
	}
	if !a.Policy().Available {
		result.Result = SubmitResult{Status: "manual_required", Reason: "sub2api_submission_unconfirmed"}
	}
	return result, nil
}
