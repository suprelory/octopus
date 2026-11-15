package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/bestruirui/octopus/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPPlatformContracts(t *testing.T) {
	now := time.UnixMilli(1700000000123).UTC()
	for _, tc := range []struct{ kind, reply string }{
		{"webhook", ""}, {"telegram", `{"ok":true}`}, {"bark", `{"code":200}`}, {"serverchan", `{"code":0}`}, {"feishu", `{"code":0}`}, {"dingtalk", `{"errcode":0}`}, {"wecom", `{"errcode":0}`}, {"ntfy", `{"id":"notice","event":"message","topic":"octopus"}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c := Connection{URL: "https://example.invalid/hook?access_token=private-url-key", Token: "12345:private-bot", ChatID: "-1234", SendKey: "SCTprivate", Secret: "example-secret", Topic: "octopus"}
			if tc.kind == "webhook" {
				c.Headers = []model.CheckinHeader{{Name: "X-Token", Value: "custom-secret"}}
				c.Payload = `{"text":"{{message}}","title":"{{title}}","nested":["{{site}}"],"number":9007199254740993}`
			}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "POST" {
					t.Fatal("notification did not use POST")
				}
				if tc.kind == "serverchan" {
					if r.URL.String() != "https://sctapi.ftqq.com/SCTprivate.send" || r.ParseForm() != nil || r.Form.Get("title") != "余额通知" || r.Form.Get("desp") != "Body\nwith \"quotes\"" {
						t.Fatal("invalid ServerChan form")
					}
				} else {
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					str := func(key string) string { var value string; _ = json.Unmarshal(body[key], &value); return value }
					switch tc.kind {
					case "webhook":
						if str("text") != "Body\nwith \"quotes\"" || str("title") != "余额通知" || string(body["nested"]) != `["Example\"\n{{title}}"]` || string(body["number"]) != "9007199254740993" || r.Header.Get("X-Token") != "custom-secret" {
							t.Fatal("Webhook interpolation changed structure, numbers, or recursively expanded user input")
						}
					case "telegram":
						if r.URL.String() != "https://api.telegram.org/bot12345:private-bot/sendMessage" || str("chat_id") != "-1234" || str("text") != "余额通知\n\nBody\nwith \"quotes\"" || body["parse_mode"] != nil {
							t.Fatal("invalid Telegram request")
						}
					case "bark":
						if str("title") != "余额通知" || str("group") != "Octopus" || str("body") == "" {
							t.Fatal("invalid Bark request")
						}
					case "feishu":
						if str("msg_type") != "text" || str("timestamp") != "1700000000" || str("sign") != "Gs3YRutIJpVN2FvEXKXHU6bEj2pVdSriQ0niig9EbUk=" {
							t.Fatal("invalid Feishu request or signing vector")
						}
					case "dingtalk":
						if str("msgtype") != "text" || r.URL.Query().Get("access_token") != "private-url-key" || r.URL.Query().Get("timestamp") != "1700000000123" || r.URL.Query().Get("sign") != "xXwpyNJ1QMREzfUrRnwpMuYxm/QF3oDrNr2iZSKyWEA=" {
							t.Fatal("invalid DingTalk request or signing vector")
						}
					case "wecom":
						if str("msgtype") != "text" || r.URL.Query().Get("timestamp") != "" {
							t.Fatal("invalid WeCom request")
						}
					case "ntfy":
						if r.URL.String() != "https://ntfy.sh" || str("topic") != "octopus" || str("title") != "余额通知" || r.Header.Get("Title") != "" || r.Header.Get("Authorization") != "Bearer 12345:private-bot" {
							t.Fatal("invalid ntfy JSON publish request")
						}
					}
				}
				status := 200
				if tc.kind == "webhook" {
					status = 204
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.reply)), Request: r}, nil
			})}
			result := Deliver(context.Background(), client, tc.kind, c, "余额通知", "Body\nwith \"quotes\"", map[string]string{"site": "Example\"\n{{title}}"}, now)
			if result.Status != "sent" || calls != 1 {
				t.Fatalf("delivery contract failed: %+v", result)
			}
		})
	}
}

func TestPlatformResponsesMustExplicitlyConfirmDelivery(t *testing.T) {
	for _, kind := range []string{"telegram", "bark", "serverchan", "feishu", "dingtalk", "wecom", "ntfy"} {
		for _, body := range []string{`{}`, `null`, `<html>private-login-page</html>`, `{"code":null,"errcode":null,"ok":null}`, `{"code":"0","errcode":"0","ok":"true"}`, strings.Repeat("a", 65537)} {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			result := Deliver(context.Background(), client, kind, Connection{URL: "https://example.invalid/secret", Token: "123:secret", ChatID: "12", SendKey: "SCTsecret", Topic: "topic"}, "test", "body", nil, time.Now())
			if result.Status != "uncertain" || result.Reason != "invalid_response" {
				t.Fatalf("%s accepted missing or malformed success evidence: %+v", kind, result)
			}
		}
	}
}

func TestHTTPFailuresRedirectsAndRateLimitsAreBoundedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		status   int
		expected string
	}{{400, "failed"}, {401, "failed"}, {429, "queued"}, {500, "uncertain"}, {408, "uncertain"}} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {"9999999999999999999999"}}, Body: io.NopCloser(strings.NewReader("private-error-with-token"))}, nil
		})}
		result := Deliver(context.Background(), client, "webhook", Connection{URL: "https://example.invalid/private"}, "test", "body", nil, time.Now())
		if result.Status != tc.expected || strings.Contains(result.Reason, "private") || result.RetryAfter > time.Hour {
			t.Fatalf("unbounded or unredacted response: %+v", result)
		}
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("private-credential-and-URL") })}
	if result := Deliver(context.Background(), client, "webhook", Connection{URL: "https://example.invalid"}, "test", "body", nil, time.Now()); result.Status != "uncertain" || result.Reason != "network_uncertain" {
		t.Fatalf("transport error not redacted: %+v", result)
	}
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
	defer redirect.Close()
	result := Deliver(context.Background(), redirect.Client(), "webhook", Connection{URL: redirect.URL, Headers: []model.CheckinHeader{{Name: "Authorization", Value: "secret"}}}, "test", "body", nil, time.Now())
	if result.Reason != "redirect_blocked" || leaked.Load() != 0 {
		t.Fatal("notification followed a redirect")
	}
}

func TestUnicodeLimitsAndFixedPrecisionTemplates(t *testing.T) {
	value := strings.Repeat("中文😀", 2000)
	utf16Text, byteText := truncateUTF16(value, 4000), truncateBytes(value, 2048)
	if !utf8.ValidString(utf16Text) || len(utf16.Encode([]rune(utf16Text))) > 4000 || !utf8.ValidString(byteText) || len(byteText) > 2048 || !strings.HasSuffix(utf16Text, "…") {
		t.Fatal("truncation broke Unicode or the platform limit")
	}
	min, max, zero := int64(-9223372036854775808), int64(9223372036854775807), int64(0)
	for _, tc := range []struct {
		input    *int64
		expected string
	}{{nil, "?"}, {&min, "-9223372036854.775808"}, {&max, "9223372036854.775807"}, {&zero, "0"}} {
		if micros(tc.input) != tc.expected {
			t.Fatal("amount rendering lost precision")
		}
	}
	c := model.CheckinNotificationConfig{TitleTemplate: "{{site}}\n{{event}}", BodyTemplate: "{{account}} {{balance}} {{reward}}"}
	title, body, err := Render(c, model.CheckinNotificationEvent{Kind: "success", SiteName: "Example\r\nBcc: example", AccountName: "{{balance}}", BalanceMicros: &zero})
	if err != nil || strings.ContainsAny(title, "\r\n\x00") || body != "{{balance}} 0 ?" {
		t.Fatal("unsafe, recursive, or imprecise template rendering", err)
	}
	for _, source := range []string{"{{secret}}", "{{ missing", "{{site.foo}}", "{{title}}"} {
		if _, _, err := Render(model.CheckinNotificationConfig{BodyTemplate: source}, model.CheckinNotificationEvent{}); err == nil {
			t.Fatal("accepted an unknown or malformed template")
		}
	}
}

func TestConnectionValidationRejectsInjectionsAndInsecureSMTPAuth(t *testing.T) {
	for _, c := range []Connection{
		{URL: "file:///private"}, {URL: "https://user:pass@example.invalid"}, {URL: "https://example.invalid/#secret"},
		{URL: "https://example.invalid/", Headers: []model.CheckinHeader{{Name: "Authorization", Value: "value\r\nInjected: secret"}}},
		{URL: "https://example.invalid/", Headers: []model.CheckinHeader{{Name: "Host", Value: "other"}}},
		{URL: "https://example.invalid/", Payload: `{"{{title}}":"wrong"}`},
	} {
		if NormalizeConnection("webhook", &c) == nil {
			t.Fatal("accepted invalid Webhook configuration")
		}
	}
	for _, mutate := range []func(*SMTPConfig){
		func(c *SMTPConfig) { c.Username, c.Password = "user", "pass" },
		func(c *SMTPConfig) { c.From = "a@example.com\r\nBcc: b@example.com" },
		func(c *SMTPConfig) { c.To = []string{"a@example.com\r\nBcc: b@example.com"} },
	} {
		c := SMTPConfig{Host: "localhost", TLS: "none", From: "a@example.com", To: []string{"b@example.com"}}
		mutate(&c)
		if NormalizeConnection("smtp", &Connection{SMTP: &c}) == nil {
			t.Fatal("accepted insecure or injected SMTP configuration")
		}
	}
}
