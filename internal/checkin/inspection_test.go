package checkin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

func inspectAccount(t *testing.T, h *harness, account model.CheckinAccount, kind string) model.CheckinAccount {
	t.Helper()
	var result model.CheckinAccount
	err := h.s.Access(func() error {
		var err error
		result, err = h.s.InspectAccount(context.Background(), account.ID, kind)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Inspection == nil || result.Inspection.FinishedAt == nil {
		t.Fatal("inspection was not completed")
	}
	return result
}

func TestNewAPIInspectionUsesReadOnlyContractsAndSeparateLedger(t *testing.T) {
	h := newHarness(t)
	h.now = time.Date(2030, 11, 14, 23, 30, 0, 0, time.UTC)
	var profileReads, statusReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Cookie") != "session=private-inspection" || r.UserAgent() != "Checkin-Adapter-Test/1.0" {
			t.Error("inspection changed the authentication/request contract")
		}
		switch r.URL.Path {
		case "/console/api/user/self":
			profileReads.Add(1)
			jsonReply(w, `{"success":true,"data":{"id":42,"status":1,"quota":500000,"used_quota":0,"email":"private-profile@example.invalid"}}`)
		case "/console/api/user/checkin":
			statusReads.Add(1)
			if r.Header.Get("New-Api-User") != "42" || r.URL.Query().Get("month") != "2030-11" {
				t.Error("native status lost its verified user or target month")
			}
			jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":true,"records":[{"checkin_date":"2030-11-15","quota_awarded":9999}]}}}`)
		default:
			t.Errorf("unexpected inspection request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	site, account := h.platformAccount(server.URL+"/console", "new-api", model.CheckinCredentials{Type: "cookie", Cookie: "session=private-inspection"})
	site.Config.Timezone = "Asia/Shanghai"
	var err error
	site, err = h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config)
	if err != nil {
		t.Fatal(err)
	}
	result := inspectAccount(t, h, account, "detect")
	if result.Inspection.Authentication != "valid" || result.Inspection.Status != "success" || result.Inspection.CheckinStatus != "checked_in" || result.Inspection.CheckinDate != "2030-11-15" {
		t.Fatalf("incorrect native inspection: %+v", result.Inspection)
	}
	if result.Balance == nil || result.Balance.AmountMicros == nil || *result.Balance.AmountMicros != 500000_000000 || result.Balance.Unit != "quota" || result.Balance.UsedMicros == nil || *result.Balance.UsedMicros != 0 {
		t.Fatalf("quota or explicit zero was lost: %+v", result.Balance)
	}
	if result.LastRunID != "" || result.LastStatus != "" || result.CredentialVersion != account.CredentialVersion || result.AutoCheckin {
		t.Fatal("inspection modified check-in state or credentials")
	}
	for _, table := range []any{&model.CheckinRun{}, &model.CheckinAttempt{}, &model.CheckinJob{}, &model.CheckinJobItem{}} {
		var count int64
		if err := h.s.Store.DB.Model(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("read-only inspection created a daily record or task", err)
		}
	}
	currentSite, _ := h.s.Store.Site(context.Background(), site.ID)
	if currentSite.Support != site.Support || currentSite.VerifiedAt != nil {
		t.Fatal("inspection changed automatic check-in capability state")
	}
	result = inspectAccount(t, h, account, "balance")
	if profileReads.Load() != 2 || statusReads.Load() != 1 || len(result.Inspection.Steps) != 1 || result.Inspection.CheckinSupport != "not_checked" {
		t.Fatal("balance refresh performed extra status/check-in requests")
	}
	public, _ := json.Marshal(result)
	if !strings.Contains(string(public), `"amount_micros":"500000000000"`) || strings.Contains(string(public), "private-inspection") || strings.Contains(string(public), "private-profile") {
		t.Fatal("observation serialization lost precision or exposed upstream data")
	}
}

func TestAccountProfilePrecisionAndUnknownValues(t *testing.T) {
	for _, tc := range []struct {
		name, platform, fields, amount, amountState, used, usedState, unit string
	}{
		{"quota", "new-api", `"quota":500000,"used_quota":12`, "500000000000", "known", "12000000", "known", "quota"},
		{"usd", "sub2api", `"balance":12.3456785,"total_recharged":100`, "12345679", "known", "", "unknown", "USD"},
		{"zero", "sub2api", `"balance":0`, "0", "known", "", "unknown", "USD"},
		{"debt", "sub2api", `"balance":"-0.0000005"`, "-1", "known", "", "unknown", "USD"},
		{"max", "sub2api", `"balance":9223372036854.775807`, "9223372036854775807", "known", "", "unknown", "USD"},
		{"min", "sub2api", `"balance":-9223372036854.775808`, "-9223372036854775808", "known", "", "unknown", "USD"},
		{"overflow", "sub2api", `"balance":9223372036854.775808`, "", "invalid", "", "unknown", "USD"},
		{"underflow", "sub2api", `"balance":-9223372036854.775809`, "", "invalid", "", "unknown", "USD"},
		{"missing", "new-api", `"request_count":3`, "", "unknown", "", "unknown", "quota"},
		{"null", "new-api", `"quota":null,"used_quota":null`, "", "unknown", "", "unknown", "quota"},
		{"invalid", "sub2api", `"balance":"NaN"`, "", "invalid", "", "unknown", "USD"},
		{"boolean", "new-api", `"quota":false,"used_quota":-1`, "", "invalid", "", "invalid", "quota"},
		{"exponent", "sub2api", `"balance":1e-6`, "1", "known", "", "unknown", "USD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelope := `"success":true`
			if tc.platform == "sub2api" {
				envelope = `"code":0`
			}
			body := `{` + envelope + `,"data":{"id":42,` + tc.fields + `}}`
			_, balance, err := inspectProfile(tc.platform, model.CheckinCredentials{}, HTTPResult{StatusCode: 200, Body: []byte(body)}, time.Now().UTC())
			if err != nil || balance == nil {
				t.Fatal("valid account rejected", err)
			}
			micros := func(value *int64) string {
				if value == nil {
					return ""
				}
				return fmt.Sprint(*value)
			}
			if micros(balance.AmountMicros) != tc.amount || balance.AmountState != tc.amountState || micros(balance.UsedMicros) != tc.used || balance.UsedState != tc.usedState || balance.Unit != tc.unit {
				t.Fatalf("incorrect amount semantics: %+v", balance)
			}
		})
	}
}

func TestInspectionRejectsUnverifiedProfilesAndKeepsLastBalance(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason, authentication string
		status                             int
	}{
		{"expired", `{"success":false,"message":"private-expired-token"}`, "invalid_credentials", "invalid", 401},
		{"mismatched ID", `{"success":true,"data":{"id":43,"quota":99}}`, "account_identity_mismatch", "invalid", 200},
		{"missing ID", `{"success":true,"data":{"quota":99}}`, "account_response_invalid", "unknown", 200},
		{"disabled", `{"success":true,"data":{"id":42,"status":2,"quota":99}}`, "account_unavailable", "invalid", 200},
		{"contradictory envelope", `{"success":true,"code":1,"data":{"id":42,"quota":99}}`, "account_response_invalid", "unknown", 200},
		{"HTML", `<html>private-login-page</html>`, "account_response_invalid", "unknown", 200},
		{"verification", `<html>Cloudflare challenge</html>`, "verification_required", "unknown", 403},
		{"missing route", `{}`, "unsupported_endpoint", "unknown", 404},
		{"limited", `{}`, "rate_limited", "unknown", 429},
		{"server error", `{}`, "upstream_http_error", "unknown", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			var rejected atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/user/self" || r.Method != "GET" {
					t.Error("an unverified profile triggered another operation")
				}
				if !rejected.Load() {
					jsonReply(w, `{"success":true,"data":{"id":42,"quota":1}}`)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "cookie", Cookie: "session=private-token", UserID: "42"})
			before := inspectAccount(t, h, account, "balance")
			rejected.Store(true)
			h.now = h.now.Add(time.Minute)
			after := inspectAccount(t, h, account, "detect")
			if after.Inspection.Status != "failed" || after.Inspection.Reason != tc.reason || after.Inspection.Authentication != tc.authentication || after.Inspection.BalanceUpdated || !reflect.DeepEqual(before.Balance, after.Balance) {
				t.Fatalf("failed read replaced/validated the old balance: %+v", after.Inspection)
			}
			encoded, _ := json.Marshal(after)
			if strings.Contains(string(encoded), "private-") {
				t.Fatal("inspection saved an upstream body or credential")
			}
		})
	}
}

func TestInspectionNeverExecutesCustomRulesOrRouterLogin(t *testing.T) {
	for _, platform := range []string{"new-api", "sub2api", "anyrouter", "agentrouter", "http", "one-api"} {
		t.Run(platform, func(t *testing.T) {
			h := newHarness(t)
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || (r.URL.Path != "/api/user/self" && r.URL.Path != "/api/v1/auth/me") {
					t.Errorf("account detection invoked a custom or login action: %s %s", r.Method, r.URL.Path)
				}
				reads.Add(1)
				jsonReply(w, `{"success":true,"code":0,"data":{"id":42,"quota":0,"balance":0}}`)
			}))
			defer server.Close()
			credentials := model.CheckinCredentials{Type: "cookie", Cookie: "session=x"}
			if platform == "anyrouter" || platform == "agentrouter" {
				credentials = model.CheckinCredentials{Type: "password", Username: "private-user", Password: "private-password"}
			}
			site, account := h.platformAccount(server.URL, platform, credentials)
			if platform == "new-api" || platform == "sub2api" {
				site.Config.Submit = model.CheckinRequest{Method: "GET", Path: "/mutating-get"}
				site.Config.Status = &model.CheckinStatusRule{Request: model.CheckinRequest{Method: "POST", Path: "/api/user/login"}, Checked: model.CheckinMatch{Path: "checked", Value: json.RawMessage("true")}, Unchecked: model.CheckinMatch{Path: "checked", Value: json.RawMessage("false")}}
				if _, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config); err != nil {
					t.Fatal(err)
				}
			}
			result := inspectAccount(t, h, account, "detect")
			if platform == "new-api" || platform == "sub2api" {
				if reads.Load() != 1 || result.Inspection.Status != "success" || result.Inspection.CheckinSupport != "not_checked" {
					t.Fatal("custom status should be skipped while verifying the account")
				}
			} else if reads.Load() != 0 || result.Inspection.Status != "unsupported" {
				t.Fatal("an unsupported platform must not guess account/login endpoints")
			}
		})
	}
}

func TestSub2APIInspectionRefreshesOnlyOnce(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint("expired=", expired), func(t *testing.T) {
			var refreshes, reads atomic.Int32
			h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.UserAgent() != "Sub2API-Test/1.0" {
					t.Error("inspection changed User-Agent")
				}
				switch r.URL.Path {
				case "/api/v1/auth/refresh":
					refreshes.Add(1)
					if r.Method != "POST" || r.Header.Get("Authorization") != "" {
						t.Error("incorrect refresh authentication")
					}
					sub2APITokens(w)
				case "/api/v1/auth/me":
					reads.Add(1)
					if r.Method != "GET" {
						t.Error("account read must be GET")
					}
					if r.Header.Get("Authorization") == "Bearer old-access" {
						sub2APIReply(w, 401, map[string]any{"code": 401, "reason": "TOKEN_EXPIRED"})
						return
					}
					jsonReply(w, `{"code":0,"data":{"id":42,"status":"active","balance":12.3456785}}`)
				default:
					t.Errorf("inspection invoked %s", r.URL.Path)
				}
			}, true, expired)
			result := inspectAccount(t, h, account, "detect")
			wantReads := int32(2)
			if expired {
				wantReads = 1
			}
			if refreshes.Load() != 1 || reads.Load() != wantReads || result.Inspection.Status != "success" || len(result.Inspection.Steps) != int(wantReads)+1 || *result.Balance.AmountMicros != 12345679 || result.Balance.Unit != "USD" || result.Balance.UsedMicros != nil {
				t.Fatalf("refresh/query contract failed: %+v", result.Inspection)
			}
			if result.CredentialVersion != account.CredentialVersion+1 || result.IdentityVersion() != account.IdentityVersion() || result.RefreshPending {
				t.Fatal("inspection failed to atomically rotate the original session")
			}
			inspectAccount(t, h, account, "balance")
			if refreshes.Load() != 1 || reads.Load() != wantReads+1 {
				t.Fatal("subsequent balance query rotated an already-fresh session")
			}
		})
	}
}

func TestInspectionSharesLeaseAndPreservesPendingCheckin(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var refreshes, submits atomic.Int32
	var confirmed atomic.Bool
	h, site, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshes.Add(1)
			close(entered)
			<-release
			sub2APITokens(w)
		case "/api/v1/auth/me":
			jsonReply(w, `{"code":0,"data":{"id":42,"balance":4}}`)
		case "/submit":
			submits.Add(1)
			jsonReply(w, `{"code":0}`)
		case "/status":
			if submits.Load() == 0 || confirmed.Load() {
				sub2APIStatus(w, confirmed.Load())
			} else {
				jsonReply(w, `{}`)
			}
		}
	}, true, false)
	expires := h.now.Add(10 * time.Minute)
	var err error
	account, err = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, CredentialVersion: account.CredentialVersion, Credentials: &model.CheckinCredentials{Type: "bearer", AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: &expires}})
	if err != nil {
		t.Fatal(err)
	}
	h.enqueue(account, "manual")
	h.drain()
	before := h.run(account)
	beforeAccount, _ := h.s.Store.Account(context.Background(), account.ID)
	if before.Status != "pending" || before.SubmitIntentAt == nil {
		t.Fatal("fixture did not establish a pending submission")
	}
	h.now = h.now.Add(15 * time.Minute)
	completed := make(chan error, 1)
	go func() {
		completed <- h.s.Access(func() error { _, err := h.s.InspectAccount(context.Background(), account.ID, "balance"); return err })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("inspection did not enter refresh")
	}
	_, conflict := h.s.InspectAccount(context.Background(), account.ID, "detect")
	_, editErr := h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision})
	h.enqueue(account, "manual")
	workerStarted := h.s.ProcessOne(context.Background())
	backupDone := make(chan error, 1)
	go func() {
		backupDone <- h.s.Exclusive(context.Background(), func() error {
			current, err := h.s.Store.Account(context.Background(), account.ID)
			if err == nil && (current.LeaseUntil != nil || current.Inspection.FinishedAt == nil) {
				return errors.New("backup raced with an active inspection")
			}
			return err
		})
	}()
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if err := <-backupDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(conflict, op.ErrCheckinConflict) || !errors.Is(editErr, op.ErrCheckinConflict) || workerStarted || refreshes.Load() != 1 {
		t.Fatal("inspection did not serialize account edits, refresh and check-in")
	}
	after := h.run(account)
	afterAccount, _ := h.s.Store.Account(context.Background(), account.ID)
	if !reflect.DeepEqual(before, after) || afterAccount.LastStatus != beforeAccount.LastStatus || afterAccount.LastRunID != beforeAccount.LastRunID || !reflect.DeepEqual(afterAccount.NextRunAt, beforeAccount.NextRunAt) || afterAccount.FailureCount != beforeAccount.FailureCount {
		t.Fatal("balance query changed a daily ledger or scheduling state")
	}
	confirmed.Store(true)
	h.drain()
	if h.run(account).Status != "success" || refreshes.Load() != 1 || submits.Load() != 1 {
		t.Fatal("confirmation after inspection lost its identity or replayed submission")
	}
}

func TestInspectionRetainsReceivedTokensUntilSavingSucceeds(t *testing.T) {
	var refreshes atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshes.Add(1)
			sub2APITokens(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Error("profile did not reuse the recovered token pair")
		}
		jsonReply(w, `{"code":0,"data":{"id":42,"balance":0}}`)
	}, false, true)
	var deny atomic.Bool
	deny.Store(true)
	callback := "test:inspection_session_save"
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && deny.Load() && values["credential_cipher"] != nil {
			tx.AddError(errors.New("temporary save failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.s.Store.DB.Callback().Update().Remove(callback)
	failed := inspectAccount(t, h, account, "balance")
	if failed.Inspection.Reason != "sub2api_refresh_save_failed" || !failed.RefreshPending || failed.Balance != nil {
		t.Fatal("unsaved token pair was discarded or reported as a successful query")
	}
	deny.Store(false)
	result := inspectAccount(t, h, account, "balance")
	if refreshes.Load() != 1 || result.Inspection.Status != "success" || result.RefreshPending || result.CredentialVersion != account.CredentialVersion+1 {
		t.Fatal("retry repeated refresh instead of persisting the received pair")
	}
}

func TestInspectionPreservesRefreshUncertaintyAndBackoff(t *testing.T) {
	for _, status := range []int{200, 429, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var refreshes atomic.Int32
			h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/auth/refresh" {
					t.Error("failed refresh allowed another operation")
				}
				refreshes.Add(1)
				sub2APIReply(w, status, map[string]any{"code": 0, "data": map[string]any{"access_token": "incomplete"}})
			}, false, true)
			first := inspectAccount(t, h, account, "detect")
			store, vault := h.s.Store, h.s.Vault
			_ = h.s.Close()
			h.s = New(store, vault)
			h.s.Now = func() time.Time { return h.now }
			second := inspectAccount(t, h, account, "balance")
			if refreshes.Load() != 1 || first.Inspection.Status != "failed" || second.Inspection.Reason != first.Inspection.Reason || second.RefreshPending != (status == 200) || second.CredentialVersion != account.CredentialVersion {
				t.Fatal("inspection retried an uncertain/rejected/rate-limited refresh after restart")
			}
		})
	}
}

func TestInspectionCancellationStillSavesRotatedTokens(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reads, refreshes atomic.Int32
	h, _, account := newSub2APIHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshes.Add(1)
			cancel()
			sub2APITokens(w)
			return
		}
		reads.Add(1)
		jsonReply(w, `{"code":0,"data":{"id":42,"balance":1}}`)
	}, false, true)
	result, err := h.s.InspectAccount(ctx, account.ID, "balance")
	if err != nil || result.Inspection.Status != "failed" || result.RefreshPending || result.CredentialVersion != account.CredentialVersion+1 || reads.Load() != 0 {
		t.Fatal("cancellation discarded received tokens or initiated another request", err)
	}
	latest, _ := h.s.Store.Account(context.Background(), account.ID)
	if latest.LeaseUntil != nil || latest.Inspection.FinishedAt == nil {
		t.Fatal("canceled query did not persist its result and release its lease")
	}
	inspectAccount(t, h, account, "balance")
	if refreshes.Load() != 1 || reads.Load() != 1 {
		t.Fatal("cancellation consumed the same refresh token twice")
	}
}

func TestInterruptedInspectionRecoversWithoutReplayingReads(t *testing.T) {
	h := newHarness(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/api/user/self" {
			jsonReply(w, `{"success":true,"data":{"id":42,"quota":1}}`)
		} else {
			jsonReply(w, `{"success":true,"data":{"enabled":true,"stats":{"checked_in_today":false,"records":[]}}}`)
		}
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "cookie", Cookie: "session=x"})
	callback := "test:inspection_final_save"
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*model.CheckinAccount); ok && row.Inspection != nil && row.Inspection.FinishedAt != nil {
			tx.AddError(errors.New("final result storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.s.Store.DB.Callback().Update().Remove(callback)
	if _, err := h.s.InspectAccount(context.Background(), account.ID, "detect"); err == nil {
		t.Fatal("failed final write was reported as completed")
	}
	current, _ := h.s.Store.Account(context.Background(), account.ID)
	if current.Inspection.Status != "running" || current.Inspection.FinishedAt != nil || len(current.Inspection.Steps) != 2 || current.Balance != nil {
		t.Fatal("intermediate audit falsely completed an inspection")
	}
	_ = h.s.Store.DB.Callback().Update().Remove(callback)
	h.now = h.now.Add(4 * time.Minute)
	if err := h.s.Store.Recover(context.Background(), h.now); err != nil {
		t.Fatal(err)
	}
	current, _ = h.s.Store.Account(context.Background(), account.ID)
	if current.Inspection.Status != "interrupted" || current.Inspection.FinishedAt == nil || current.LeaseUntil != nil || requests.Load() != 2 {
		t.Fatal("inspection recovery lost the interruption or replayed a request")
	}
}

func TestInspectionSnapshotInvalidationAndUnknownRefresh(t *testing.T) {
	h := newHarness(t)
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if missing.Load() {
			jsonReply(w, `{"success":true,"data":{"id":42,"quota":null}}`)
		} else {
			jsonReply(w, `{"success":true,"data":{"id":42,"quota":1}}`)
		}
	}))
	defer server.Close()
	site, account := h.platformAccount(server.URL, "new-api", model.CheckinCredentials{Type: "cookie", Cookie: "session=x"})
	observed := inspectAccount(t, h, account, "balance")
	name := "Renamed after reading"
	// The form's account was loaded before the new observation was written.
	edited, err := h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: account.Revision, Name: &name})
	if err != nil || !reflect.DeepEqual(edited.Balance, observed.Balance) || !reflect.DeepEqual(edited.Inspection, observed.Inspection) {
		t.Fatal("a stale metadata edit overwrote the latest observation", err)
	}
	missing.Store(true)
	unknown := inspectAccount(t, h, account, "balance")
	if unknown.Inspection.Status != "partial" || !unknown.Inspection.BalanceUpdated || unknown.Balance.AmountMicros != nil || unknown.Balance.AmountState != "unknown" {
		t.Fatal("missing data became zero or retained an obsolete amount")
	}
	edited, err = h.s.SaveAccount(context.Background(), site.ID, account.ID, AccountWrite{Revision: edited.Revision, CredentialVersion: edited.CredentialVersion, Credentials: &model.CheckinCredentials{Type: "cookie", Cookie: "session=replacement"}})
	if err != nil || edited.Balance != nil || edited.Inspection != nil {
		t.Fatal("replacement credentials retained the old account observation", err)
	}
	inspectAccount(t, h, edited, "balance")
	site.Config.BaseURL += "/new-tenant"
	if _, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config); err != nil {
		t.Fatal(err)
	}
	current, _ := h.s.Store.Account(context.Background(), account.ID)
	if current.Balance != nil || current.Inspection != nil {
		t.Fatal("a different upstream tenant retained the old balance")
	}
}
