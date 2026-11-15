package checkin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

func TestAgentRouterLoginIsTheOnlyMutationAndNeverInfersReward(t *testing.T) {
	for _, mode := range []string{"explicit", "log", "earlier log", "no session"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			var logins atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/user/login":
					requireSubmitMarker(t, h)
					if r.Method != "POST" {
						t.Error("login must be POST")
					}
					logins.Add(1)
					if mode != "no session" {
						http.SetCookie(w, &http.Cookie{Name: "session", Value: "agent-private", Path: "/"})
					}
					if mode == "log" {
						jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":false,"quota":999999,"reward":999}}`)
					} else {
						jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":true,"quota":999999,"reward":999}}`)
					}
				case "/api/log/self/":
					if r.Method != "GET" || r.Header.Get("Cookie") != "session=agent-private" || r.Header.Get("New-Api-User") != "42" {
						t.Error("log query lost the login session")
					}
					if mode == "explicit" {
						jsonReply(w, `{"success":true,"data":{"items":[]}}`)
						return
					}
					at := h.now.Unix()
					if mode == "earlier log" {
						at -= 60
					}
					jsonReply(w, agentLog(at))
				default:
					t.Error("AgentRouter used a generic check-in endpoint", r.URL.Path)
				}
			}))
			defer server.Close()
			_, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "agent@example.com", Password: "agent-private-password"})
			h.enqueue(account, "manual")
			h.drain()
			run := h.run(account)
			want := "success"
			if mode == "earlier log" {
				want = "already_checked_in"
			}
			if run.Status != want || run.RewardMicros != nil || run.RewardState != "unknown" || run.SubmitIntentAt == nil {
				t.Fatalf("wrong login check-in result: %+v", run)
			}
			h.enqueue(account, "manual")
			h.drain()
			if logins.Load() != 1 {
				t.Fatal("confirmed login check-in repeated")
			}
		})
	}
}

func TestAgentRouterLoginSuccessAloneAndUnrelatedLogsRemainUnconfirmed(t *testing.T) {
	for _, checked := range []string{"missing", "false", "string"} {
		t.Run(checked, func(t *testing.T) {
			h := newHarness(t)
			var logins atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/user/login" {
					logins.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "agent", Path: "/"})
					body := `{"success":true,"data":{"id":42,"quota":99999}}`
					if checked == "false" {
						body = `{"success":true,"data":{"id":42,"checked_in":false}}`
					}
					if checked == "string" {
						body = `{"success":true,"data":{"id":42,"checked_in":"true"}}`
					}
					jsonReply(w, body)
					return
				}
				jsonReply(w, `{"success":true,"data":{"items":[{"type":4,"content":"balance adjustment","created_at":1920888000}]}}`)
			}))
			defer server.Close()
			_, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "agent", Password: "secret"})
			h.enqueue(account, "manual")
			h.drain()
			run := h.run(account)
			if run.Status != "pending" || run.SafeToRetry || run.RewardMicros != nil {
				t.Fatal("login or balance was treated as confirmed check-in", run)
			}
			h.enqueue(account, "manual")
			h.drain()
			h.now = h.now.Add(time.Minute)
			if err := h.s.Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.drain()
			if logins.Load() != 1 {
				t.Fatal("manual or automatic confirmation logged in again")
			}
		})
	}
}

func TestAgentRouterMarkerFailurePreventsLogin(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); jsonReply(w, `{"success":true}`) }))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "u", Password: "p"})
	callback := "test:block_agent_intent"
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && values["phase"] == "submit_intent" {
			tx.AddError(errors.New("intent unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.s.Store.DB.Callback().Update().Remove(callback)
	h.enqueue(account, "manual")
	h.s.ProcessOne(context.Background())
	if calls.Load() != 0 {
		t.Fatal("login ran without a durable intent")
	}
}

func TestAgentRouterSessionSaveRetriesDoNotRepeatLogin(t *testing.T) {
	h := newHarness(t)
	var logins atomic.Int32
	var reject atomic.Bool
	reject.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/login" {
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "new-session", Path: "/"})
			jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":true}}`)
			return
		}
		if r.Header.Get("Cookie") != "session=new-session" {
			t.Error("save recovery lost the received cookie")
		}
		jsonReply(w, agentLog(h.now.Unix()))
	}))
	defer server.Close()
	site, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "u", Password: "p"})
	before := fingerprint(site, account)
	callback := "test:block_login_save"
	if err := h.s.Store.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && values["session_saved_at"] != nil && reject.Load() {
			tx.AddError(errors.New("session save unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer h.s.Store.DB.Callback().Update().Remove(callback)
	h.enqueue(account, "manual")
	h.drain()
	if h.run(account).Reason != "login_session_save_failed" || h.run(account).SubmitIntentAt == nil {
		t.Fatal("save failure lost the durable login intent")
	}
	reject.Store(false)
	h.enqueue(account, "manual")
	h.drain()
	latest, credentials := storedCredentials(t, h, account)
	if h.run(account).Status != "success" || logins.Load() != 1 || credentials.Session == nil || latest.CredentialUserID != "42" || fingerprint(site, latest) != before {
		t.Fatal("session save recovery replayed login or changed identity")
	}
}

func TestAgentRouterLostLoginResponseRecoversByLogsAfterRestart(t *testing.T) {
	h := newHarness(t)
	var logins atomic.Int32
	var visible atomic.Bool
	var secondDay atomic.Bool
	var targetStamp int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/login" {
			logins.Add(1)
			requireSubmitMarker(t, h)
			if secondDay.Load() {
				visible.Store(true)
				connection, _, _ := w.(http.Hijacker).Hijack()
				_ = connection.Close()
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "previous-session", Path: "/"})
			jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":true}}`)
			return
		}
		if visible.Load() {
			jsonReply(w, agentLog(targetStamp))
			return
		}
		jsonReply(w, `{"success":true,"data":{"items":[]}}`)
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "u", Password: "p"})
	h.enqueue(account, "manual")
	h.drain()
	h.now = h.now.Add(24 * time.Hour)
	targetStamp = h.now.Unix()
	secondDay.Store(true)
	h.enqueue(account, "manual")
	h.drain()
	runs, _ := h.s.Store.Runs(context.Background(), op.CheckinRunFilter{AccountID: account.ID})
	if len(runs) != 2 || runs[0].SubmitIntentAt == nil || runs[0].Status == "success" {
		t.Fatal("lost login response was not treated as uncertain")
	}
	old := h.s
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	h.s = New(old.Store, old.Vault)
	h.s.Now = func() time.Time { return h.now }
	h.now = h.now.Add(24 * time.Hour)
	if _, err := h.s.Confirm(context.Background(), runs[0].ID, "", false); err != nil {
		t.Fatal(err)
	}
	h.drain()
	run, _ := h.s.Store.Run(context.Background(), runs[0].ID)
	if run.Status != "success" || logins.Load() != 2 || run.RewardMicros != nil {
		t.Fatal("recovery logged in again or failed to use dated logs", run)
	}
}

func TestAgentRouterLogsRejectWrongDayAndUnrelatedEvidence(t *testing.T) {
	start := time.Date(2030, 11, 14, 0, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Hour)
	for _, test := range []struct {
		body  string
		valid bool
	}{
		{agentLog(start.Unix() - 1), false}, {agentLog(end.Unix() + 1), false}, {agentLog(start.Unix()), true},
		{`{"success":true,"data":{"items":[{"type":4,"content":"充值成功","created_at":1920844800}]}}`, false},
		{`{"success":true,"data":{"items":[{"type":4,"content":"签到成功","created_at":"1920844800"}]}}`, false},
		{`{"success":false,"data":{"items":[{"type":4,"content":"签到成功","created_at":1920844800}]}}`, false},
	} {
		if valid := agentRouterLogTime(HTTPResult{StatusCode: 200, Body: []byte(test.body)}, start, end) != nil; valid != test.valid {
			t.Fatal("invalid log evidence accepted", test.body)
		}
	}
}
