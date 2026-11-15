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
	source      string
}

func (r *adapterRuntime) request(ctx context.Context, stage string, config model.CheckinRequest, auth model.CheckinCredentials) (HTTPResult, error) {
	return r.requestBody(ctx, stage, config, auth, true)
}

func (r *adapterRuntime) requestBody(ctx context.Context, stage string, config model.CheckinRequest, auth model.CheckinCredentials, expand bool) (HTTPResult, error) {
	started := time.Now()
	result := doRequestHTTP(ctx, r.client, r.site.Config, config, auth, r.run.CheckinDate, expand)
	reason, message := "response_received", "Upstream response received"
	if result.Err != nil {
		reason, message = "network_error", "Request failed, timed out, or exceeded the response limit"
	}
	// Never persist bodies, headers, URLs or credential-derived text.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := r.s.Store.AddAttempt(saveCtx, model.CheckinAttempt{RunID: r.run.ID, JobID: r.item.JobID, Stage: stage, HTTPStatus: result.StatusCode, Reason: reason, Message: message, DurationMS: time.Since(started).Milliseconds(), CreatedAt: r.s.Now()})
	return result, err
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
	refreshed, err := a.r.s.ensureSub2APISession(ctx, a.r.client, a.r.site, a.r.item, a.r.account, a.r.credentials, force)
	a.refreshed = a.refreshed || refreshed
	return err
}
func (a *sub2APIAdapter) Prepare(ctx context.Context, _ bool) error { return a.refresh(ctx, false) }
func (a *sub2APIAdapter) Query(ctx context.Context, stage string) (statusResult, error) {
	result, err := a.requestAdapter.Query(ctx, stage)
	if err != nil || a.r.credentials.Type != "bearer" {
		return result, err
	}
	if sub2APIRecoverable(result.HTTP) && !a.refreshed {
		if err := a.refresh(ctx, true); err != nil {
			return result, err
		}
		result, err = a.requestAdapter.Query(ctx, stage)
	}
	if err == nil && result.HTTP.Err == nil && (result.HTTP.StatusCode == 401 || result.HTTP.StatusCode == 403) {
		err = sub2APIRejected(result.HTTP)
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
