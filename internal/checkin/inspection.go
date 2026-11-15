package checkin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// InspectAccount is called inside Access, like the other management operations.
// It shares worker capacity, site pacing, and the durable account/session lease.
func (s *Service) InspectAccount(parent context.Context, accountID, kind string) (model.CheckinAccount, error) {
	settings, err := s.Store.Settings(parent)
	if err != nil {
		return model.CheckinAccount{}, err
	}
	account, site, err := s.claimInspection(parent, accountID, kind, settings)
	if err != nil {
		return account, err
	}
	defer func() {
		s.mu.Lock()
		delete(s.active, account.ID)
		delete(s.activeSites, site.ID)
		s.nextSite[site.ID] = s.Now().Add(time.Duration(settings.SiteIntervalSeconds) * time.Second)
		s.mu.Unlock()
		s.Notify("account")
		s.wg.Done()
	}()
	ctx, cancel := context.WithTimeout(parent, checkinOperationTimeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	s.Notify("account")
	inspection := account.Inspection
	runtime := &adapterRuntime{
		s: s, site: site, account: &account, credentials: &model.CheckinCredentials{}, inspection: inspection,
		item: model.CheckinJobItem{AccountID: account.ID, SiteID: site.ID, LeaseOwner: account.LeaseOwner, CheckinDate: inspection.CheckinDate},
	}
	balance, operationErr := s.inspect(ctx, runtime, settings)
	if operationErr != nil {
		inspection.Status, inspection.Reason = "failed", "inspection_failed"
		if balance != nil {
			inspection.Status = "partial"
		}
		if _, reason, recognized := adapterErrorState(operationErr); recognized {
			inspection.Reason = reason
			operationErr = nil // An upstream failure is a saved diagnostic result.
		}
		switch inspection.Reason {
		case "invalid_credentials", "account_identity_mismatch", "account_unavailable", "sub2api_refresh_required", "sub2api_refresh_rejected", "sub2api_auth_rejected", "sub2api_session_revoked", "sub2api_account_unavailable":
			inspection.Authentication = "invalid"
		}
	}
	finished := s.Now()
	inspection.FinishedAt = &finished
	inspection.BalanceUpdated = balance != nil
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	if err := s.Store.SaveInspection(saveCtx, runtime.item, account, inspection, balance, true, s.Now()); err != nil {
		return account, err // The expired lease will recover the unfinished observation.
	}
	account.Inspection = inspection
	if balance != nil {
		account.Balance = balance
	}
	account.LeaseOwner, account.LeaseUntil = "", nil
	s.notifyBalance(saveCtx, site, account)
	return account, operationErr
}

func (s *Service) claimInspection(ctx context.Context, accountID, kind string, settings model.CheckinSettings) (model.CheckinAccount, model.CheckinSite, error) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	account, err := s.Store.Account(ctx, accountID)
	if err != nil {
		return account, model.CheckinSite{}, err
	}
	s.mu.Lock()
	conflict := s.active[account.ID]
	busy := s.closed || s.paused || len(s.active) >= settings.Concurrency || s.activeSites[account.SiteID] || s.Now().Before(s.nextSite[account.SiteID])
	s.mu.Unlock()
	if conflict {
		return account, model.CheckinSite{}, op.ErrCheckinConflict
	}
	if busy {
		return account, model.CheckinSite{}, op.ErrCheckinBusy
	}
	account, site, err := s.Store.BeginInspection(ctx, accountID, kind, s.Now())
	if err != nil {
		return account, site, err
	}
	s.mu.Lock()
	if !s.closed && !s.paused {
		s.active[account.ID], s.activeSites[site.ID] = true, true
		s.wg.Add(1)
		s.mu.Unlock()
		return account, site, nil
	}
	s.mu.Unlock()
	now := s.Now()
	account.Inspection.Status, account.Inspection.Reason, account.Inspection.FinishedAt = "interrupted", "inspection_interrupted", &now
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = s.Store.SaveInspection(saveCtx, model.CheckinJobItem{LeaseOwner: account.LeaseOwner}, account, account.Inspection, nil, true, now)
	if err != nil {
		return account, site, err
	}
	return account, site, op.ErrCheckinBusy
}

func (s *Service) inspect(ctx context.Context, r *adapterRuntime, settings model.CheckinSettings) (*model.CheckinBalanceSnapshot, error) {
	inspection := r.inspection
	platform, exists := model.CheckinPlatformByID(r.site.Config.Adapter)
	if !exists || (inspection.Kind == "detect" && !platform.AccountDetection) || (inspection.Kind == "balance" && !platform.BalanceQuery) {
		inspection.Status, inspection.Reason = "unsupported", "inspection_unsupported"
		return nil, nil
	}
	proxy, err := op.ResolveProxy(ctx, s.Store.DB, r.account.ProxySelection(r.site.Config))
	if err != nil {
		return nil, needsAttention("invalid_proxy")
	}
	plain, err := s.Vault.Unseal(r.account.ID, r.account.CredentialCipher)
	if err != nil {
		return nil, needsAttention("credential_unavailable")
	}
	if json.Unmarshal(plain, r.credentials) != nil || model.ValidateCheckinCredentials(*r.credentials) != nil || !model.CheckinCredentialSupported(platform.ID, r.credentials.Type) {
		return nil, needsAttention("invalid_credentials")
	}
	r.client, err = newHTTPClient(proxy.URL, time.Duration(settings.HTTPTimeoutSeconds)*time.Second)
	if err != nil {
		return nil, needsAttention("invalid_proxy")
	}
	defer r.client.CloseIdleConnections()
	var response HTTPResult
	if platform.ID == "sub2api" {
		adapter := &sub2APIAdapter{requestAdapter: requestAdapter{r}}
		if err := adapter.Prepare(ctx, true); err != nil {
			return nil, err
		}
		response, err = adapter.readRequest(ctx, "account", model.CheckinRequest{Method: http.MethodGet, Path: "/api/v1/auth/me"})
	} else {
		response, err = r.request(ctx, "account", model.CheckinRequest{Method: http.MethodGet, Path: "/api/user/self"}, *r.credentials)
	}
	if err != nil {
		return nil, err
	}
	userID, balance, err := inspectProfile(platform.ID, *r.credentials, response, s.Now())
	if err != nil {
		return nil, err
	}
	inspection.Authentication, inspection.UserID = "valid", userID
	inspection.Status, inspection.Reason = "success", "account_verified"
	if balance.AmountState != "known" {
		inspection.Status, inspection.Reason = "partial", "balance_unknown"
	}
	if balance.AmountState == "invalid" || balance.UsedState == "invalid" {
		inspection.Status, inspection.Reason = "partial", "balance_invalid"
	}
	// Only the known native GET contract is probed. Custom rules may contain
	// arbitrary GET/POST side effects and are never invoked by account detection.
	if inspection.Kind != "detect" || platform.ID != "new-api" || !model.CheckinNativeStatus(r.site.Config) {
		return balance, nil
	}
	if r.credentials.UserID == "" {
		r.credentials.UserID = userID // This response verified the ID; no enumeration.
	}
	response, err = r.request(ctx, "detect_status", model.CheckinRequest{Method: http.MethodGet, Path: "/api/user/checkin?month=" + inspection.CheckinDate[:7]}, *r.credentials)
	if err != nil {
		return balance, err
	}
	inspection.CheckinSupport = "unknown"
	data, err := inspectionResponse(platform.ID, response)
	if err != nil {
		inspection.Status = "partial"
		_, inspection.Reason, _ = adapterErrorState(err)
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
			inspection.CheckinSupport = "unsupported"
		}
		return balance, nil
	}
	if matches(data, model.CheckinMatch{Path: "data.enabled", Value: json.RawMessage("false")}) {
		inspection.CheckinSupport, inspection.Status, inspection.Reason = "disabled", "partial", "checkin_disabled"
		return balance, nil
	}
	if matches(data, model.CheckinMatch{Path: "data.enabled", Value: json.RawMessage("true")}) {
		inspection.CheckinSupport = "supported"
	}
	inspection.CheckinStatus = newAPIStatus(response, inspection.CheckinDate, businessDate(r.site.Config, s.Now())).State
	if inspection.CheckinStatus == "unknown" {
		inspection.Status, inspection.Reason = "partial", "checkin_status_unknown"
	}
	return balance, nil
}

func inspectionResponse(platform string, response HTTPResult) (any, error) {
	if response.Err != nil {
		return nil, needsAttention("network_error")
	}
	data, decodeErr := decodeObject(response.Body)
	success := model.CheckinMatch{Path: "success", Value: json.RawMessage("true")}
	if platform == "sub2api" {
		success = model.CheckinMatch{Path: "code", Value: json.RawMessage("0")}
	}
	validEnvelope := decodeErr == nil && matches(data, success) && routerSuccess(data)
	if !validEnvelope && routerChallenge(response, data) {
		return nil, needsAttention("verification_required")
	}
	switch {
	case response.StatusCode == 401 || response.StatusCode == 403:
		return nil, needsAttention("invalid_credentials")
	case response.StatusCode == 404 || response.StatusCode == 405:
		return nil, needsAttention("unsupported_endpoint")
	case response.StatusCode == 429:
		return nil, needsAttention("rate_limited")
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, needsAttention("upstream_http_error")
	case !validEnvelope:
		if decodeErr == nil && routerAuthMismatch(data) && matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("false")}) {
			return nil, needsAttention("invalid_credentials")
		}
		return nil, needsAttention("account_response_invalid")
	}
	return data, nil
}

func inspectProfile(platform string, credentials model.CheckinCredentials, response HTTPResult, now time.Time) (string, *model.CheckinBalanceSnapshot, error) {
	data, err := inspectionResponse(platform, response)
	if err != nil {
		return "", nil, err
	}
	id, _ := field(data, "data.id")
	userID := routerUserID(id)
	if userID == "" {
		return "", nil, needsAttention("account_response_invalid")
	}
	if credentials.UserID != "" && routerUserID(credentials.UserID) != userID {
		return "", nil, needsAttention("account_identity_mismatch")
	}
	if platform == "sub2api" && credentials.Type == "bearer" {
		if expected, exists := field(sub2APIJWTData(credentials.AccessToken), "user_id"); exists && routerUserID(expected) != userID {
			return "", nil, needsAttention("account_identity_mismatch")
		}
	}
	if status, exists := field(data, "data.status"); exists {
		if platform == "sub2api" {
			if _, ok := status.(string); !ok {
				return "", nil, needsAttention("account_response_invalid")
			}
			if status != "active" {
				return "", nil, needsAttention("account_unavailable")
			}
		} else {
			if _, ok := status.(json.Number); !ok {
				return "", nil, needsAttention("account_response_invalid")
			}
			if !matches(data, model.CheckinMatch{Path: "data.status", Value: json.RawMessage("1")}) {
				return "", nil, needsAttention("account_unavailable")
			}
		}
	}
	balance := &model.CheckinBalanceSnapshot{Unit: "quota", UpdatedAt: now, UsedState: "unknown"}
	path := "data.quota"
	if platform == "sub2api" {
		balance.Unit, path = "USD", "data.balance"
	} else {
		balance.UsedMicros, balance.UsedState = accountAmount(data, "data.used_quota", false)
	}
	balance.AmountMicros, balance.AmountState = accountAmount(data, path, true)
	return userID, balance, nil
}

func accountAmount(data any, path string, signed bool) (*int64, string) {
	value, exists := field(data, path)
	if !exists || value == nil {
		return nil, "unknown"
	}
	var decimal string
	switch v := value.(type) {
	case json.Number:
		decimal = v.String()
	case string:
		decimal = strings.TrimSpace(v)
	default:
		return nil, "invalid"
	}
	micros, err := scaledDecimalMicros(decimal, "1", signed)
	if err != nil {
		return nil, "invalid"
	}
	return micros, "known"
}
