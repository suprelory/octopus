package checkin

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"golang.org/x/net/publicsuffix"
)

type anyRouterAdapter struct {
	requestAdapter
	session *routerSession
}

func newAnyRouterAdapter(r *adapterRuntime) platformAdapter {
	return &anyRouterAdapter{requestAdapter: requestAdapter{r}}
}

func (a *anyRouterAdapter) Prepare(ctx context.Context, readOnly bool) error {
	var err error
	a.session, err = openRouterSession(ctx, a.r)
	if err != nil {
		return err
	}
	if a.session.hasAuth("/api/user/self") {
		response, err := a.verify(ctx, a.session)
		if err != nil {
			return err
		}
		if routerSuccessResponse(response) {
			return a.session.persist(ctx)
		}
		data, _ := decodeObject(response.Body)
		if a.r.credentials.UserID == "" && strings.Contains(routerMessage(data), "new-api-user") && routerCanFallback(response) {
			return needsAttention("router_user_id_required")
		}
		if !routerCanFallback(response) {
			return needsAttention(routerSubmitResult(response).Reason)
		}
		if ok, err := a.alternativeAuth(ctx); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
	if a.r.credentials.Type == "password" && !readOnly {
		if _, _, err := a.session.login(ctx, "authenticate"); err != nil {
			return err
		}
		if a.session.hasAuth("/api/user/self") {
			return nil
		}
	}
	return needsAttention("router_session_required")
}

func routerSuccessResponse(response HTTPResult) bool {
	data, err := decodeObject(response.Body)
	return response.Err == nil && response.StatusCode >= 200 && response.StatusCode < 300 && err == nil && !routerChallenge(response, data) && routerSuccess(data)
}

func (a *anyRouterAdapter) verify(ctx context.Context, session *routerSession) (HTTPResult, error) {
	response, err := session.request(ctx, "authenticate", model.CheckinRequest{Method: "GET", Path: "/api/user/self"}, false)
	if err != nil {
		return response, err
	}
	if routerSuccessResponse(response) {
		data, _ := decodeObject(response.Body)
		if err := session.bindUserID(routerResponseUserID(data)); err != nil {
			return response, err
		}
	}
	return response, nil
}

func (s *routerSession) clone() *routerSession {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	copy := &routerSession{r: s.r, jar: jar, cookies: map[string]model.CheckinSessionCookie{}, auth: s.auth, token: s.token, savedPlain: s.savedPlain}
	u, _ := url.Parse(s.r.site.Config.BaseURL)
	for _, cookie := range s.cookies {
		copy.receive(u, []*http.Cookie{{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, Expires: cookie.Expires}})
	}
	return copy
}

// Only the account's own token is tried as a session/token cookie, and only on
// read-only /self first. No user IDs are guessed or taken from other accounts.
func (a *anyRouterAdapter) alternativeAuth(ctx context.Context) (bool, error) {
	if a.session.token == "" {
		return false, nil
	}
	modes := []string{"session", "token"}
	if a.session.auth == "cookie" {
		modes = []string{"bearer"}
	}
	for _, mode := range modes {
		candidate := a.session.clone()
		candidate.auth = "bearer"
		if mode != "bearer" {
			candidate.auth = "cookie"
			candidate.seedCookie(mode + "=" + candidate.token)
		}
		response, err := a.verify(ctx, candidate)
		if err != nil {
			return false, err
		}
		if routerSuccessResponse(response) {
			a.session = candidate
			return true, candidate.persist(ctx)
		}
		if !routerCanFallback(response) {
			return false, needsAttention(routerSubmitResult(response).Reason)
		}
	}
	return false, nil
}

func (a *anyRouterAdapter) Query(ctx context.Context, stage string) (statusResult, error) {
	rule := a.r.site.Config.Status
	if rule == nil || !a.queryDateValid() {
		return statusResult{State: "unknown"}, nil
	}
	response, err := a.session.request(ctx, stage, rule.Request, false)
	if err != nil {
		return statusResult{}, err
	}
	if err := a.session.persist(ctx); err != nil {
		return statusResult{}, err
	}
	result := a.status(response)
	data, decodeErr := decodeObject(response.Body)
	if decodeErr != nil || routerChallenge(response, data) || !routerSuccess(data) {
		result.State = "unknown"
	}
	return result, nil
}

func (a *anyRouterAdapter) Submit(ctx context.Context) (submission, error) {
	var rejected *submission
	for authAttempt := 0; authAttempt < 2; authAttempt++ {
		paths := []string{"/api/user/checkin"}
		if a.session.auth == "cookie" {
			paths = []string{"/api/user/sign_in", "/api/user/checkin"}
		}
		for _, path := range paths {
			if ctx.Err() != nil {
				return submission{}, needsAttention("network_error")
			}
			// A fallback may not move a daily mutation into the next business day.
			if businessDate(a.r.site.Config, a.r.s.Now()) != a.r.run.CheckinDate || (a.r.source == "scheduled" && !inWindow(a.r.site.Config, a.r.s.Now())) {
				return submission{}, needsAttention("date_expired")
			}
			response, err := a.session.request(ctx, "submit", model.CheckinRequest{Method: "POST", Path: path, Body: "{}", Headers: []model.CheckinHeader{{Name: "X-Requested-With", Value: "XMLHttpRequest"}}}, false)
			if err != nil {
				return submission{}, err
			}
			if err := a.session.persist(ctx); err != nil {
				return submission{}, err
			}
			result := submission{HTTP: response, Result: routerSubmitResult(response)}
			if !routerCanFallback(response) {
				return result, nil
			}
			// A missing alternative route must not hide an authentication failure.
			if rejected == nil || rejected.Result.Reason == "unsupported_endpoint" {
				rejected = &result
			}
		}
		if authAttempt == 1 {
			break
		}
		if ok, err := a.alternativeAuth(ctx); err != nil {
			return submission{}, err
		} else if !ok {
			break
		}
	}
	if rejected != nil {
		return *rejected, nil
	}
	return submission{Result: SubmitResult{Status: "manual_required", Reason: "unconfirmed_response"}}, nil
}
