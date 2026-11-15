package checkin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"golang.org/x/net/publicsuffix"
)

type pendingLoginSession struct {
	before     model.CheckinAccount
	cipher     string
	userID     string
	hasSession bool
}

func (r *adapterRuntime) savePendingLogin(ctx context.Context, pending pendingLoginSession) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		err := r.s.Store.SaveLoginSession(saveCtx, r.item, pending.before, pending.cipher, pending.userID, pending.hasSession, r.s.Now())
		latest, readErr := r.s.Store.Account(saveCtx, r.account.ID)
		if readErr == nil && latest.CredentialVersion == pending.before.CredentialVersion+1 && latest.CredentialCipher == pending.cipher {
			plain, err := r.s.Vault.Unseal(latest.ID, latest.CredentialCipher)
			if err != nil || json.Unmarshal(plain, r.credentials) != nil {
				return needsAttention("credential_unavailable")
			}
			*r.account = latest
			r.s.sessionMu.Lock()
			delete(r.s.pendingLogins, latest.ID)
			r.s.sessionMu.Unlock()
			return nil
		}
		if readErr == nil && latest.CredentialVersion != pending.before.CredentialVersion {
			r.s.sessionMu.Lock()
			delete(r.s.pendingLogins, latest.ID)
			r.s.sessionMu.Unlock()
			return needsAttention("config_changed")
		}
		if err == nil || !waitContext(saveCtx, time.Duration(attempt+1)*100*time.Millisecond) {
			break
		}
	}
	return &adapterError{"pending", "login_session_save_failed"}
}

type routerSession struct {
	r          *adapterRuntime
	jar        *cookiejar.Jar
	cookies    map[string]model.CheckinSessionCookie
	auth       string
	token      string
	savedPlain string
}

func openRouterSession(ctx context.Context, r *adapterRuntime) (*routerSession, error) {
	r.s.sessionMu.Lock()
	pending, exists := r.s.pendingLogins[r.account.ID]
	if exists && pending.before.CredentialVersion != r.account.CredentialVersion {
		delete(r.s.pendingLogins, r.account.ID)
		exists = false
	}
	r.s.sessionMu.Unlock()
	if exists {
		if err := r.savePendingLogin(ctx, pending); err != nil {
			return nil, err
		}
	}
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	s := &routerSession{r: r, jar: jar, cookies: map[string]model.CheckinSessionCookie{}, auth: "cookie"}
	plain, _ := json.Marshal(r.credentials)
	s.savedPlain = string(plain)
	u, _ := url.Parse(r.site.Config.BaseURL)
	if saved := r.credentials.Session; saved != nil && saved.BaseURL == r.site.Config.BaseURL {
		s.auth, s.token = saved.Auth, saved.AccessToken
		for _, cookie := range saved.Cookies {
			s.receive(u, []*http.Cookie{{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, Expires: cookie.Expires}})
		}
	} else if r.credentials.Type == "cookie" {
		s.seedCookie(r.credentials.Cookie)
	} else if r.credentials.Type == "bearer" {
		s.auth, s.token = "bearer", stripBearer(r.credentials.AccessToken)
	}
	return s, nil
}

func stripBearer(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		value = strings.TrimSpace(value[7:])
	}
	return value
}

func routerUserID(value any) string {
	var raw string
	switch value := value.(type) {
	case string:
		raw = strings.TrimSpace(value)
	case json.Number:
		raw = value.String()
	default:
		return ""
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func routerResponseUserID(data any) string {
	var id string
	for _, path := range []string{"data.id", "data.user.id", "data.user_id"} {
		if value, ok := field(data, path); ok {
			candidate := routerUserID(value)
			if candidate == "" || (id != "" && candidate != id) {
				return ""
			}
			id = candidate
		}
	}
	return id
}

func (s *routerSession) bindUserID(id string) error {
	if id == "" {
		return needsAttention("router_user_id_required")
	}
	if previous := s.r.credentials.UserID; previous != "" && previous != id {
		return needsAttention("router_identity_mismatch")
	}
	s.r.credentials.UserID = id
	return nil
}

func (s *routerSession) seedCookie(header string) {
	u, _ := url.Parse(s.r.site.Config.BaseURL)
	request := &http.Request{Header: http.Header{"Cookie": []string{header}}}
	for _, cookie := range request.Cookies() {
		cookie.Path = "/"
		s.receive(u, []*http.Cookie{cookie})
	}
}

func (s *routerSession) receive(u *url.URL, cookies []*http.Cookie) {
	for _, raw := range cookies {
		cookie := *raw
		if cookie.Valid() != nil {
			continue
		}
		domain := strings.ToLower(strings.TrimPrefix(cookie.Domain, "."))
		host := strings.ToLower(u.Hostname())
		if domain != "" && domain != host && (!strings.HasSuffix(host, "."+domain) || publicsuffix.List.PublicSuffix(domain) == domain) {
			continue
		}
		if cookie.Path == "" || !strings.HasPrefix(cookie.Path, "/") {
			cookie.Path = "/"
			if i := strings.LastIndex(u.Path, "/"); i > 0 {
				cookie.Path = u.Path[:i]
			}
		}
		key := domain
		if key == "" {
			key = host
		}
		key += "\n" + cookie.Path + "\n" + cookie.Name
		if cookie.MaxAge > 0 {
			cookie.Expires = s.r.s.Now().Add(time.Duration(cookie.MaxAge) * time.Second)
			cookie.MaxAge = 0
		}
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !cookie.Expires.After(s.r.s.Now())) {
			delete(s.cookies, key)
			cookie.MaxAge = -1
		} else {
			s.cookies[key] = model.CheckinSessionCookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, Expires: cookie.Expires}
		}
		s.jar.SetCookies(u, []*http.Cookie{&cookie})
	}
}

func usableRouterCookie(cookies []*http.Cookie) bool {
	for _, cookie := range cookies {
		name := strings.ToLower(cookie.Name)
		if cookie.Value != "" && (strings.Contains(name, "session") || strings.Contains(name, "token") || strings.Contains(name, "auth") || name == "jwt") {
			return true
		}
	}
	return false
}

func (s *routerSession) credential(path string) model.CheckinCredentials {
	auth := model.CheckinCredentials{Type: s.auth, AccessToken: s.token, UserID: s.r.credentials.UserID}
	if s.auth == "cookie" {
		u, _ := checkinRequestURL(s.r.site.Config.BaseURL, path, s.r.run.CheckinDate)
		request := &http.Request{Header: make(http.Header)}
		for _, cookie := range s.jar.Cookies(u) {
			request.AddCookie(cookie)
		}
		auth.Cookie, auth.AccessToken = request.Header.Get("Cookie"), ""
	}
	return auth
}

func (s *routerSession) hasAuth(path string) bool {
	if s.auth == "bearer" {
		return s.token != ""
	}
	u, _ := checkinRequestURL(s.r.site.Config.BaseURL, path, s.r.run.CheckinDate)
	return usableRouterCookie(s.jar.Cookies(u))
}

func (s *routerSession) request(ctx context.Context, stage string, config model.CheckinRequest, anonymous bool) (HTTPResult, error) {
	auth := s.credential(config.Path)
	if anonymous {
		auth = model.CheckinCredentials{}
	}
	response, err := s.r.requestBody(ctx, stage, config, auth, !anonymous)
	if response.URL != nil {
		s.receive(response.URL, response.Cookies)
	}
	return response, err
}

func (s *routerSession) persist(ctx context.Context) error {
	snapshot := &model.CheckinLoginSession{BaseURL: s.r.site.Config.BaseURL, Auth: s.auth, AccessToken: s.token, Cookies: []model.CheckinSessionCookie{}}
	keys := make([]string, 0, len(s.cookies))
	for key := range s.cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		snapshot.Cookies = append(snapshot.Cookies, s.cookies[key])
	}
	if model.ValidateCheckinLoginSession(*snapshot) != nil {
		return needsAttention("invalid_login_session")
	}
	credentials := *s.r.credentials
	credentials.Session = snapshot
	plain, _ := json.Marshal(credentials)
	if string(plain) == s.savedPlain {
		return nil
	}
	cipher, err := s.r.s.Vault.Seal(s.r.account.ID, plain)
	if err != nil {
		return err
	}
	pending := pendingLoginSession{before: *s.r.account, cipher: cipher, userID: credentials.UserID, hasSession: s.hasAuth("/api/user/self")}
	s.r.s.sessionMu.Lock()
	if s.r.s.pendingLogins == nil {
		s.r.s.pendingLogins = map[string]pendingLoginSession{}
	}
	s.r.s.pendingLogins[s.r.account.ID] = pending
	s.r.s.sessionMu.Unlock()
	if err := s.r.savePendingLogin(ctx, pending); err != nil {
		return err
	}
	s.savedPlain = string(plain)
	return nil
}

// A successful login must identify the same user. Challenge responses sometimes
// carry success:true too; they do not establish a usable session.
func (s *routerSession) login(ctx context.Context, stage string) (HTTPResult, any, error) {
	body, _ := json.Marshal(map[string]string{"username": s.r.credentials.Username, "password": s.r.credentials.Password})
	response, requestErr := s.request(ctx, stage, model.CheckinRequest{Method: "POST", Path: "/api/user/login", Body: string(body), Headers: []model.CheckinHeader{{Name: "X-Requested-With", Value: "XMLHttpRequest"}}}, true)
	data, decodeErr := decodeObject(response.Body)
	// Even when recording the attempt fails, save a valid received session before
	// yielding to lease recovery. Replaying a login is never a storage retry.
	if requestErr != nil && (response.Err != nil || decodeErr != nil) {
		return response, nil, requestErr
	}
	if response.Err != nil {
		return response, nil, needsAttention("network_error")
	}
	if routerChallenge(response, data) {
		return response, nil, needsAttention("verification_required")
	}
	if response.StatusCode == 429 {
		return response, nil, needsAttention("rate_limited")
	}
	if response.StatusCode >= 500 {
		return response, nil, needsAttention("upstream_http_error")
	}
	if decodeErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 || !routerSuccess(data) || !matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("true")}) {
		return response, nil, needsAttention("router_login_unconfirmed")
	}
	if err := s.bindUserID(routerResponseUserID(data)); err != nil {
		return response, nil, err
	}
	s.auth, s.token = "cookie", ""
	for _, path := range []string{"data.access_token", "data.token", "access_token", "token"} {
		if value, ok := field(data, path); ok {
			if token, ok := value.(string); ok && token != "" {
				s.token = stripBearer(token)
				break
			}
		}
	}
	newCookie := false
	u, _ := checkinRequestURL(s.r.site.Config.BaseURL, "/api/user/self", s.r.run.CheckinDate)
	for _, received := range response.Cookies {
		if received.MaxAge < 0 || !usableRouterCookie([]*http.Cookie{received}) {
			continue
		}
		for _, accepted := range s.jar.Cookies(u) {
			if received.Name == accepted.Name && received.Value == accepted.Value {
				newCookie = true
			}
		}
	}
	if !newCookie && s.token != "" {
		s.auth = "bearer"
	}
	if err := s.persist(ctx); err != nil {
		return response, data, err
	}
	return response, data, requestErr
}
