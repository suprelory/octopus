package checkin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestRouterCookiesKeepScopeAndDeletedCookiesStayDeleted(t *testing.T) {
	h := newHarness(t)
	site, account := h.platformAccount("https://router.example.invalid", "anyrouter", model.CheckinCredentials{Type: "cookie", Cookie: "session=original"})
	_, credentials := storedCredentials(t, h, account)
	r := &adapterRuntime{s: h.s, site: site, account: &account, credentials: &credentials, run: &model.CheckinRun{CheckinDate: "2030-11-14"}}
	session, err := openRouterSession(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(site.Config.BaseURL + "/api/user/self")
	session.receive(u, []*http.Cookie{
		{Name: "session", Value: "rotated", Path: "/", Secure: true},
		{Name: "path_token", Value: "only-user", Path: "/api/user/"},
		{Name: "foreign_token", Value: "wrong-domain", Domain: "elsewhere.invalid", Path: "/"},
		{Name: "expired_token", Value: "expired", Path: "/", Expires: h.now.Add(-time.Second)},
	})
	if cookie := session.credential("/api/log/self/").Cookie; cookie != "session=rotated" {
		t.Fatal("cookie scope expanded", cookie)
	}
	if cookie := session.credential("/api/user/self").Cookie; !strings.Contains(cookie, "path_token=only-user") || strings.Contains(cookie, "wrong-domain") {
		t.Fatal("path/domain scope incorrect", cookie)
	}
	session.receive(u, []*http.Cookie{{Name: "session", Path: "/", MaxAge: -1}})
	// Reopen from an encrypted-session-shaped snapshot; the initial Cookie must
	// not be seeded again after the upstream removed it.
	snapshot := &model.CheckinLoginSession{BaseURL: site.Config.BaseURL, Auth: "cookie"}
	for _, cookie := range session.cookies {
		snapshot.Cookies = append(snapshot.Cookies, cookie)
	}
	r.credentials.Session = snapshot
	reopened, err := openRouterSession(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.hasAuth("/api/log/self/") || strings.Contains(reopened.credential("/api/user/self").Cookie, "session=") {
		t.Fatal("deleted authentication cookie was resurrected")
	}
}

func TestAgentRouterUsesReturnedBearerWhenPriorCookieIsStale(t *testing.T) {
	h := newHarness(t)
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/login" {
			if logins.Add(1) == 1 {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "stale-next-day", Path: "/"})
				jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":true}}`)
			} else {
				jsonReply(w, `{"success":true,"data":{"id":42,"checked_in":false,"access_token":"fresh-bearer"}}`)
			}
			return
		}
		if r.Header.Get("Authorization") == "Bearer fresh-bearer" {
			jsonReply(w, agentLog(h.now.Unix()))
			return
		}
		w.WriteHeader(401)
		jsonReply(w, `{"success":false}`)
	}))
	defer server.Close()
	_, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "u", Password: "p"})
	h.enqueue(account, "manual")
	h.drain()
	h.now = h.now.Add(24 * time.Hour)
	h.enqueue(account, "manual")
	h.drain()
	runs, _ := h.s.Store.Runs(context.Background(), op.CheckinRunFilter{AccountID: account.ID})
	if len(runs) != 2 || runs[0].Status != "success" || logins.Load() != 2 {
		t.Fatal("stale cookie hid returned bearer session", runs)
	}
}

func TestAgentRouterChallengeCannotBecomeCheckinOrAnotherLogin(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"data":{"require_2fa":true,"id":42,"checked_in":true}}`,
		`{"success":true,"data":{"challenge":{"type":"totp"},"id":42,"checked_in":true}}`,
		`{"success":true,"code":1,"data":{"id":42,"checked_in":true}}`,
	} {
		h := newHarness(t)
		var logins atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { logins.Add(1); jsonReply(w, body) }))
		_, account := h.platformAccount(server.URL, "agentrouter", model.CheckinCredentials{Type: "password", Username: "u", Password: "p"})
		h.enqueue(account, "manual")
		h.drain()
		if h.run(account).Status == "success" {
			t.Fatal("challenge or conflicting status accepted")
		}
		h.enqueue(account, "manual")
		h.drain()
		if logins.Load() != 1 {
			t.Fatal("confirmation repeated login after a challenge")
		}
		server.Close()
	}
}
