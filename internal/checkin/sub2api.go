package checkin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

type sub2APIAuthError struct {
	reason    string
	temporary bool
	fallback  bool
}

func (e *sub2APIAuthError) Error() string { return e.reason }

// Only encrypted replacement credentials survive a temporary database outage.
// The durable refresh marker prevents replay if the process loses this cache.
type pendingCheckinSession struct {
	before      model.CheckinAccount
	cipher      string
	expiresAt   *time.Time
	refreshedAt time.Time
}

func sub2APIJWTData(token string) any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[1]) > 16<<10 {
		return nil
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	data, err := decodeObject(body)
	if err != nil {
		return nil
	}
	return data
}

func sub2APIJWTTime(token, fieldName string) *time.Time {
	data := sub2APIJWTData(token)
	value, _ := field(data, fieldName)
	number, ok := value.(json.Number)
	if !ok {
		return nil
	}
	seconds, err := number.Int64()
	if err != nil || seconds <= 0 || seconds > 253402300799 {
		return nil
	}
	at := time.Unix(seconds, 0).UTC()
	return &at
}

func sameSub2APISession(previous, next string) bool {
	before, after := sub2APIJWTData(previous), sub2APIJWTData(next)
	// This only rejects changed claims; it never authenticates an unverified JWT.
	// The supplied Sub2API server preserves user_id and refresh-family sid.
	for _, path := range []string{"user_id", "sid"} {
		value, exists := field(before, path)
		if !exists || value == nil || value == "" {
			continue
		}
		expected, _ := json.Marshal(value)
		if !matches(after, model.CheckinMatch{Path: path, Value: expected}) {
			return false
		}
	}
	return true
}

func normalizeSub2APICredentials(credentials *model.CheckinCredentials) {
	credentials.AccessToken = strings.TrimSpace(credentials.AccessToken)
	if len(credentials.AccessToken) >= 7 && strings.EqualFold(credentials.AccessToken[:7], "Bearer ") {
		credentials.AccessToken = strings.TrimSpace(credentials.AccessToken[7:])
	}
	credentials.RefreshToken = strings.TrimSpace(credentials.RefreshToken)
	if credentials.ExpiresAt != nil {
		at := credentials.ExpiresAt.UTC()
		credentials.ExpiresAt = &at
	}
	// Unverified JWT claims only schedule refresh; they never establish identity.
	if jwtExpiry := sub2APIJWTTime(credentials.AccessToken, "exp"); jwtExpiry != nil && (credentials.ExpiresAt == nil || jwtExpiry.Before(*credentials.ExpiresAt)) {
		credentials.ExpiresAt = jwtExpiry
	}
}

func sub2APIRefreshDue(credentials model.CheckinCredentials, account model.CheckinAccount, now time.Time) bool {
	if credentials.ExpiresAt == nil {
		return false
	}
	lead := 5 * time.Minute
	issued := sub2APIJWTTime(credentials.AccessToken, "iat")
	if account.TokenRefreshedAt != nil && (issued == nil || account.TokenRefreshedAt.After(*issued)) {
		issued = account.TokenRefreshedAt
	}
	if issued != nil && credentials.ExpiresAt.After(*issued) {
		lead = min(lead, credentials.ExpiresAt.Sub(*issued)/5)
	}
	return !now.Before(credentials.ExpiresAt.Add(-lead))
}

func sub2APIResponseReason(response HTTPResult) string {
	data, err := decodeObject(response.Body)
	if err != nil {
		return ""
	}
	for _, path := range []string{"reason", "error.reason", "error.code", "code"} {
		value, _ := field(data, path)
		if reason, ok := value.(string); ok && reason != "" {
			return reason
		}
	}
	return ""
}

func sub2APIRecoverable(response HTTPResult) bool {
	if response.Err != nil || response.StatusCode != http.StatusUnauthorized {
		return false
	}
	switch sub2APIResponseReason(response) {
	case "UNAUTHORIZED", "INVALID_TOKEN", "TOKEN_EXPIRED", "ACCESS_TOKEN_EXPIRED":
		return true
	}
	return false
}

func sub2APIRejected(response HTTPResult) *sub2APIAuthError {
	reason := "sub2api_auth_rejected"
	switch sub2APIResponseReason(response) {
	case "TOKEN_REVOKED", "SESSION_REVOKED":
		reason = "sub2api_session_revoked"
	case "SESSION_BINDING_MISMATCH":
		reason = "sub2api_session_binding_mismatch"
	case "USER_NOT_FOUND", "USER_INACTIVE", "USER_NOT_ACTIVE", "USER_DISABLED":
		reason = "sub2api_account_unavailable"
	case "REFRESH_TOKEN_INVALID", "REFRESH_TOKEN_EXPIRED", "REFRESH_TOKEN_REUSED":
		return &sub2APIAuthError{reason: "sub2api_refresh_rejected", fallback: true}
	}
	return &sub2APIAuthError{reason: reason}
}

func sub2APIRefreshCredentials(response HTTPResult, old model.CheckinCredentials, now time.Time) (model.CheckinCredentials, bool) {
	data, err := decodeObject(response.Body)
	if err != nil || response.Err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return old, false
	}
	if !matches(data, model.CheckinMatch{Path: "code", Value: json.RawMessage("0")}) {
		return old, false
	}
	access, _ := field(data, "data.access_token")
	refresh, _ := field(data, "data.refresh_token")
	expires, _ := field(data, "data.expires_in")
	accessText, _ := access.(string)
	refreshText, _ := refresh.(string)
	number, ok := expires.(json.Number)
	if !ok {
		return old, false
	}
	seconds, err := number.Int64()
	if err != nil || seconds <= 0 || seconds > 10*365*24*60*60 {
		return old, false
	}
	if kind, exists := field(data, "data.token_type"); exists && kind != "Bearer" && kind != "bearer" {
		return old, false
	}
	expiry := now.Add(time.Duration(seconds) * time.Second)
	next := model.CheckinCredentials{Type: "bearer", AccessToken: accessText, RefreshToken: refreshText, UserID: old.UserID, ExpiresAt: &expiry}
	normalizeSub2APICredentials(&next)
	if model.ValidateCheckinCredentials(next) != nil || next.RefreshToken == "" || next.RefreshToken == old.RefreshToken || !next.ExpiresAt.After(now) {
		return old, false
	}
	return next, true
}

func (s *Service) persistPendingSession(ctx context.Context, item model.CheckinJobItem, account *model.CheckinAccount, credentials *model.CheckinCredentials, pending pendingCheckinSession) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		err := s.Store.SaveSessionRefresh(saveCtx, item, pending.before, pending.cipher, pending.expiresAt, pending.refreshedAt, s.Now())
		latest, readErr := s.Store.Account(saveCtx, account.ID)
		if readErr == nil && latest.CredentialCipher == pending.cipher && latest.CredentialVersion == pending.before.CredentialVersion+1 {
			plain, openErr := s.Vault.Unseal(latest.ID, latest.CredentialCipher)
			if openErr != nil || json.Unmarshal(plain, credentials) != nil {
				return &sub2APIAuthError{reason: "credential_unavailable"}
			}
			*account = latest
			s.sessionMu.Lock()
			delete(s.pendingSessions, account.ID)
			s.sessionMu.Unlock()
			return nil
		}
		if readErr == nil && latest.CredentialVersion != pending.before.CredentialVersion {
			s.sessionMu.Lock()
			delete(s.pendingSessions, account.ID)
			s.sessionMu.Unlock()
			return &sub2APIAuthError{reason: "config_changed"}
		}
		if err == nil || !waitContext(saveCtx, time.Duration(attempt+1)*100*time.Millisecond) {
			break
		}
	}
	return &sub2APIAuthError{reason: "sub2api_refresh_save_failed", temporary: true}
}

// Called only by a leased account worker. Queries may recover once; a submit
// request is never retried by this helper, regardless of its HTTP method.
func (r *adapterRuntime) ensureSub2APISession(ctx context.Context, force bool) (bool, error) {
	s, client, site, item, account, credentials := r.s, r.client, r.site, r.item, r.account, r.credentials
	normalizeSub2APICredentials(credentials)
	fallback := func(err *sub2APIAuthError) (bool, error) {
		if !force && err.fallback && credentials.ExpiresAt != nil && credentials.ExpiresAt.After(s.Now().Add(5*time.Second)) {
			return false, nil
		}
		return false, err
	}
	s.sessionMu.Lock()
	pending, exists := s.pendingSessions[account.ID]
	if exists && pending.before.CredentialVersion != account.CredentialVersion {
		delete(s.pendingSessions, account.ID)
		exists = false
	}
	s.sessionMu.Unlock()
	if exists {
		return true, s.persistPendingSession(ctx, item, account, credentials, pending)
	}
	if account.RefreshPending {
		reason := account.RefreshError
		if reason == "" {
			reason = "sub2api_refresh_uncertain"
		}
		return fallback(&sub2APIAuthError{reason: reason, fallback: reason == "sub2api_refresh_uncertain"})
	}
	if account.RefreshError != "" && account.RefreshError != "sub2api_refresh_deferred" {
		return fallback(&sub2APIAuthError{reason: account.RefreshError, fallback: account.RefreshError == "sub2api_refresh_rejected"})
	}
	if !force && !sub2APIRefreshDue(*credentials, *account, s.Now()) {
		return false, nil
	}
	if credentials.RefreshToken == "" {
		if !force && credentials.ExpiresAt != nil && credentials.ExpiresAt.After(s.Now()) {
			return false, nil
		}
		return false, &sub2APIAuthError{reason: "sub2api_refresh_required"}
	}
	if account.RefreshRetryAt != nil && s.Now().Before(*account.RefreshRetryAt) {
		return fallback(&sub2APIAuthError{reason: "sub2api_refresh_deferred", temporary: true, fallback: true})
	}
	if err := r.initTurnstile(ctx); err != nil {
		return false, err
	}
	if err := s.Store.MarkSessionRefresh(ctx, item, *account, s.Now()); err != nil {
		return false, err
	}
	account.RefreshPending = true
	// Finish an already-started rotation and save its replacement even if the
	// browser disconnects or shutdown cancels the worker. The lease lasts longer.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"refresh_token": credentials.RefreshToken})
	request := model.CheckinRequest{Method: "POST", Path: "/api/v1/auth/refresh", Body: string(body)}
	started := time.Now()
	response := doRequestHTTP(refreshCtx, client, site.Config, request, model.CheckinCredentials{}, "", false)
	duration := time.Since(started)
	if isTurnstileChallenge(response) {
		// A definite captcha refusal did not consume the refresh token. Clear the
		// rotation marker before solving, and persist it again before replaying.
		if err := r.clearRejectedRefresh(ctx); err != nil {
			return false, err
		}
		if err := r.auditReason(ctx, "refresh", response, duration, "turnstile_required"); err != nil {
			return false, err
		}
		token, err := r.solveTurnstile(ctx, response)
		if err != nil {
			return false, err
		}
		if err := r.turnstileRetryAllowed(ctx, false); err != nil {
			return false, err
		}
		if err := s.Store.MarkSessionRefresh(ctx, item, *account, s.Now()); err != nil {
			return false, err
		}
		account.RefreshPending = true
		token.cookies = response.Cookies
		retryCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		started = time.Now()
		response = doRequestHTTPWithToken(retryCtx, client, site.Config, request, model.CheckinCredentials{}, "", false, token)
		duration = time.Since(started)
		stop()
		if isTurnstileChallenge(response) {
			if err := r.clearRejectedRefresh(ctx); err != nil {
				return false, err
			}
			if err := r.auditReason(ctx, "refresh", response, duration, "turnstile_rejected"); err != nil {
				return false, err
			}
			return false, needsAttention("turnstile_rejected")
		}
	}
	next, valid := sub2APIRefreshCredentials(response, *credentials, s.Now())
	identityChanged := valid && !sameSub2APISession(credentials.AccessToken, next.AccessToken)
	valid = valid && !identityChanged
	var authErr error
	if valid {
		plain, _ := json.Marshal(next)
		cipher, err := s.Vault.Seal(account.ID, plain)
		if err != nil {
			return false, err
		}
		pending = pendingCheckinSession{before: *account, cipher: cipher, expiresAt: next.ExpiresAt, refreshedAt: s.Now()}
		s.sessionMu.Lock()
		if s.pendingSessions == nil {
			s.pendingSessions = map[string]pendingCheckinSession{}
		}
		s.pendingSessions[account.ID] = pending
		s.sessionMu.Unlock()
		authErr = s.persistPendingSession(ctx, item, account, credentials, pending)
	} else {
		failure := &sub2APIAuthError{reason: "sub2api_refresh_uncertain", fallback: true}
		var retryAt *time.Time
		uncertain := true
		if identityChanged {
			failure = &sub2APIAuthError{reason: "sub2api_refresh_identity_mismatch"}
		} else if response.Err == nil && response.StatusCode == http.StatusTooManyRequests {
			failure = &sub2APIAuthError{reason: "sub2api_refresh_deferred", temporary: true, fallback: true}
			at := s.Now().Add(time.Minute)
			retryAt, uncertain = &at, false
		} else if response.Err == nil && (response.StatusCode == 400 || response.StatusCode == 401 || response.StatusCode == 403) {
			failure, uncertain = sub2APIRejected(response), false
		}
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := s.Store.FailSessionRefresh(saveCtx, item, *account, uncertain, failure.reason, retryAt, s.Now())
		saveCancel()
		if err != nil {
			authErr = err
		} else {
			account.RefreshPending, account.RefreshError, account.RefreshRetryAt = uncertain, failure.reason, retryAt
			_, authErr = fallback(failure)
		}
	}
	if err := r.audit(ctx, "refresh", response, duration); err != nil {
		return valid, err
	}
	return valid, authErr
}

func (r *adapterRuntime) clearRejectedRefresh(ctx context.Context) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.s.Store.FailSessionRefresh(saveCtx, r.item, *r.account, false, "", nil, r.s.Now()); err != nil {
		return err
	}
	r.account.RefreshPending, r.account.RefreshError, r.account.RefreshRetryAt = false, "", nil
	return nil
}

func sub2APIErrorState(err error) (status, reason string, recognized bool) {
	var authErr *sub2APIAuthError
	if !errors.As(err, &authErr) {
		return "", "", false
	}
	status = "manual_required"
	if authErr.temporary {
		status = "pending"
	}
	return status, authErr.reason, true
}
