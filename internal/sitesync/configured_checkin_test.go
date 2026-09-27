package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestConfiguredHTTPCheckinUsesAccountAuthPathHeadersAndBody(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/root/api/checkin" || r.URL.Query().Get("day") != "today" {
			t.Errorf("request target = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if got := r.Header.Get("Cookie"); got != `session=token"slash\` {
			t.Errorf("cookie = %q", got)
		}
		if got := r.Header.Get("X-Checkin-User"); got != `Alice "A"` {
			t.Errorf("custom header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		if got := r.Header.Get("Origin"); got != server.URL {
			t.Errorf("origin = %q, want %q", got, server.URL)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body["token"] != `session=token"slash\` || body["username"] != `Alice "A"` {
			t.Errorf("request body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"reward":"8 points"}}`))
	}))
	defer server.Close()

	site := &model.Site{
		Platform:           model.SitePlatformAPI,
		BaseURL:            server.URL + "/root",
		CheckinHTTPEnabled: true,
		CheckinHTTPMethod:  "POST",
		CheckinHTTPPath:    "/api/checkin?day=today",
		CheckinHTTPBody:    `{"token":"{{access_token}}","username":"{{username}}"}`,
		CheckinHTTPHeaders: []model.CustomHeader{{HeaderKey: "X-Checkin-User", HeaderValue: "{{username}}"}},
	}
	account := &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    `session=token"slash\`,
		Username:       `Alice "A"`,
	}
	result, token, err := checkinAccountState(context.Background(), site, account)
	if err != nil {
		t.Fatalf("configured checkin failed: %v", err)
	}
	if token != account.AccessToken || result.Status != model.SiteExecutionStatusSuccess || result.Reward != "8 points" {
		t.Fatalf("unexpected result/token: result=%+v token=%q", result, token)
	}
}

func TestConfiguredHTTPCheckinInterpretsAlreadyCheckedInAndFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/already":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"success":false,"message":"今日已签到"}`))
		case "/rejected":
			_, _ = w.Write([]byte(`{"success":false,"message":"余额不足"}`))
		case "/plain":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	account := &model.SiteAccount{CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "bearer-token"}
	for _, test := range []struct {
		path        string
		wantStatus  model.SiteExecutionStatus
		wantReason  string
		wantMessage string
	}{
		{path: "/already", wantStatus: model.SiteExecutionStatusSuccess, wantReason: model.SiteCheckinReasonAlreadyCheckedIn},
		{path: "/rejected", wantStatus: model.SiteExecutionStatusFailed, wantMessage: "余额不足"},
		{path: "/plain", wantStatus: model.SiteExecutionStatusSuccess, wantReason: model.SiteCheckinReasonCheckedIn},
	} {
		site := &model.Site{
			Platform:           model.SitePlatformAPI,
			BaseURL:            server.URL,
			CheckinHTTPEnabled: true,
			CheckinHTTPMethod:  "GET",
			CheckinHTTPPath:    test.path,
		}
		result, _, err := checkinAccountState(context.Background(), site, account)
		if err != nil {
			t.Fatalf("checkin %s failed: %v", test.path, err)
		}
		if result.Status != test.wantStatus || result.Reason != test.wantReason || (test.wantMessage != "" && result.Message != test.wantMessage) {
			t.Errorf("checkin %s result = %+v", test.path, result)
		}
	}
}

func TestBuildConfiguredCheckinURLKeepsSitePrefixAndRejectsTraversal(t *testing.T) {
	got, err := buildConfiguredCheckinURL("https://example.com/root/", "/api/checkin?date=today")
	if err != nil || got != "https://example.com/root/api/checkin?date=today" {
		t.Fatalf("configured URL = %q, error=%v", got, err)
	}
	for _, path := range []string{"", "https://other.example/path", "//other.example/path", "/api/../admin"} {
		if _, err := buildConfiguredCheckinURL("https://example.com", path); err == nil {
			t.Errorf("unsafe checkin path accepted: %q", path)
		}
	}
}

func TestCheckinHTTPClientDoesNotForwardCredentialsAcrossRedirectOrigins(t *testing.T) {
	var secondServerHit bool
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondServerHit = true
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("redirect leaked Cookie header: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("redirect leaked Authorization header: %q", got)
		}
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/landing", http.StatusFound)
	}))
	defer first.Close()

	request, err := http.NewRequest(http.MethodPost, first.URL+"/checkin", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Cookie", "session=secret")
	request.Header.Set("Authorization", "Bearer secret")
	response, err := checkinHTTPClient(first.Client()).Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound || secondServerHit {
		t.Fatalf("cross-origin redirect was followed: status=%d hit=%v", response.StatusCode, secondServerHit)
	}
}
