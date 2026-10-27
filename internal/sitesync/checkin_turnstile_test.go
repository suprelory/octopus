package sitesync

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/turnstile"
)

const turnstileTestSiteKey = "0x4AAAAAAATEST"

type turnstileSite struct {
	server             *httptest.Server
	checkins, statuses atomic.Int32
}

// newTurnstileSite mimics new-api with TurnstileCheck on its check-in route:
// requests without the solved token fail with HTTP 200 and success=false.
func newTurnstileSite(t *testing.T, siteKey string) *turnstileSite {
	t.Helper()
	site := &turnstileSite{}
	site.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			site.statuses.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"turnstile_check":true,"turnstile_site_key":"` + siteKey + `"}}`))
		case "/api/user/checkin":
			site.checkins.Add(1)
			if r.URL.Query().Get("turnstile") != "solved-token" {
				_, _ = w.Write([]byte(`{"success":false,"message":"Turnstile token 为空"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"签到成功","data":{"reward":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"message":"not found"}`))
		}
	}))
	t.Cleanup(site.server.Close)
	return site
}

// newCloudflareSolverStub answers like Cloudflare Solver, or fails task creation
// with errorCode when it is set.
func newCloudflareSolverStub(t *testing.T, siteURL, token, errorCode string, solves *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer sk-solver-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"ERROR_KEY_DOES_NOT_EXIST"}`))
			return
		}
		switch r.URL.Path {
		case "/turnstile":
			solves.Add(1)
			if !strings.HasPrefix(r.URL.Query().Get("url"), siteURL) || r.URL.Query().Get("sitekey") != turnstileTestSiteKey {
				t.Errorf("unexpected solve request: %s", r.URL.RawQuery)
			}
			if errorCode != "" {
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"` + errorCode + `","errorDescription":"raw platform text"}`))
				return
			}
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":"task-1"}`))
		case "/result":
			_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"token":"` + token + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func useTurnstileSolverConfig(t *testing.T, config turnstileSolverConfig) {
	t.Helper()
	previous := loadTurnstileSolverConfig
	loadTurnstileSolverConfig = func() turnstileSolverConfig { return config }
	t.Cleanup(func() { loadTurnstileSolverConfig = previous })
}

func TestCheckinRetriesWithSolvedTurnstileToken(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site := newTurnstileSite(t, turnstileTestSiteKey)
	var solves atomic.Int32
	solver := newCloudflareSolverStub(t, site.server.URL, "solved-token", "", &solves)
	useTurnstileSolverConfig(t, turnstileSolverConfig{enabled: true, solvers: []turnstile.Solver{
		{Platform: turnstile.PlatformCloudflareSolver, APIKey: "sk-solver-secret", BaseURL: solver.URL},
	}})
	_, account := createCheckinFixture(t, ctx, site.server.URL)
	result, err := CheckinAccount(ctx, account.ID)
	if err != nil || result == nil || result.Status != model.SiteExecutionStatusSuccess || result.Reason != model.SiteCheckinReasonCheckedIn || result.Reward != "1" {
		t.Fatalf("unexpected outcome: %+v, error=%v", result, err)
	}
	if site.checkins.Load() != 2 || site.statuses.Load() != 1 || solves.Load() != 1 {
		t.Fatalf("checkins=%d statuses=%d solves=%d", site.checkins.Load(), site.statuses.Load(), solves.Load())
	}
}

func TestCheckinTurnstileRejectionUnchangedWhileSolverDisabled(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site := newTurnstileSite(t, turnstileTestSiteKey)
	useTurnstileSolverConfig(t, turnstileSolverConfig{})
	_, account := createCheckinFixture(t, ctx, site.server.URL)
	result, err := CheckinAccount(ctx, account.ID)
	if err == nil || result == nil || result.Status != model.SiteExecutionStatusFailed ||
		result.Reason != string(SiteBatchReasonUpstreamBusinessError) || result.Message != "Turnstile token 为空" {
		t.Fatalf("unexpected outcome: %+v, error=%v", result, err)
	}
	if site.checkins.Load() != 1 || site.statuses.Load() != 0 {
		t.Fatalf("checkins=%d statuses=%d", site.checkins.Load(), site.statuses.Load())
	}
}

func TestCheckinTurnstileSolverFailures(t *testing.T) {
	for _, tc := range []struct {
		name, siteKey, token, errorCode, want string
		noSolvers                             bool
		checkins, statuses, solves            int32
	}{
		{name: "no usable platform", siteKey: turnstileTestSiteKey, noSolvers: true, want: "no enabled solver platform has an API key", checkins: 1},
		{name: "missing site key", want: "the site did not publish a Turnstile site key", checkins: 1, statuses: 1},
		{name: "token rejected", siteKey: turnstileTestSiteKey, token: "stale-token", want: "the site rejected the solved token", checkins: 2, statuses: 1, solves: 1},
		{name: "solver error", siteKey: turnstileTestSiteKey, errorCode: "ERROR_ZERO_BALANCE", want: "solving failed: Cloudflare Solver: ERROR_ZERO_BALANCE", checkins: 1, statuses: 1, solves: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := setupProjectTestDB(t)
			site := newTurnstileSite(t, tc.siteKey)
			var solves atomic.Int32
			solver := newCloudflareSolverStub(t, site.server.URL, tc.token, tc.errorCode, &solves)
			config := turnstileSolverConfig{enabled: true}
			if !tc.noSolvers {
				config.solvers = []turnstile.Solver{{Platform: turnstile.PlatformCloudflareSolver, APIKey: "sk-solver-secret", BaseURL: solver.URL}}
			}
			useTurnstileSolverConfig(t, config)
			_, account := createCheckinFixture(t, ctx, site.server.URL)
			result, err := CheckinAccount(ctx, account.ID)
			want := "Turnstile token 为空 (Turnstile solver: " + tc.want + ")"
			if err == nil || result == nil || result.Status != model.SiteExecutionStatusFailed ||
				result.Reason != string(SiteBatchReasonUpstreamBusinessError) || result.Message != want {
				t.Fatalf("unexpected outcome: %+v, error=%v", result, err)
			}
			if strings.Contains(result.Message, "sk-solver-secret") || strings.Contains(result.Message, "raw platform text") {
				t.Fatalf("message leaked solver details: %q", result.Message)
			}
			if site.checkins.Load() != tc.checkins || site.statuses.Load() != tc.statuses || solves.Load() != tc.solves {
				t.Fatalf("checkins=%d statuses=%d solves=%d", site.checkins.Load(), site.statuses.Load(), solves.Load())
			}
		})
	}
}
