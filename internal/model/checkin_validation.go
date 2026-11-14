package model

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

func CheckinClock(value string) (int, error) {
	if value == "24:00" {
		return 1440, nil
	}
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, InvalidCheckin("time must use HH:MM")
	}
	return t.Hour()*60 + t.Minute(), nil
}

func ValidateCheckinURL(value string, proxy bool) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.Fragment != "" {
		return InvalidCheckin("invalid URL")
	}
	if proxy {
		if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return InvalidCheckin("proxy URL must not contain credentials, query or fragment")
		}
		if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
			return InvalidCheckin("unsupported proxy scheme")
		}
	} else if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" {
		return InvalidCheckin("site URL must use HTTP(S), without credentials, query or fragment")
	}
	return nil
}

func ValidateCheckinMatch(rule CheckinMatch) error {
	if rule.Path == "" || len(rule.Path) > 512 {
		return InvalidCheckin("rule requires a field path")
	}
	for _, part := range strings.Split(rule.Path, ".") {
		if part == "" || strings.ContainsAny(part, "\r\n\x00 []{}()#?*") {
			return InvalidCheckin("invalid field path")
		}
	}
	var v any
	if len(rule.Value) > 256 || json.Unmarshal(rule.Value, &v) != nil {
		return InvalidCheckin("rule value must be a JSON scalar")
	}
	switch v.(type) {
	case bool, string, float64:
		return nil
	}
	return InvalidCheckin("rule value must be a boolean, string or number")
}

func ValidateCheckinRequest(r CheckinRequest) error {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return InvalidCheckin("request method must be GET or POST")
	}
	p := strings.ReplaceAll(r.Path, "{{checkin_date}}", "2026-01-01")
	u, err := url.ParseRequestURI(p)
	if err != nil || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || u.IsAbs() || u.Host != "" || strings.ContainsAny(p, "\\\r\n\x00{}") {
		return InvalidCheckin("request path must stay on this site")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." {
			return InvalidCheckin("relative path segments are not allowed")
		}
	}
	if len(r.Body) > 65536 || (r.Body != "" && !json.Valid([]byte(r.Body))) {
		return InvalidCheckin("request body must be valid JSON up to 64 KiB")
	}
	if r.Body != "" {
		var body any
		if json.Unmarshal([]byte(r.Body), &body) != nil || containsCheckinSecretField(body) {
			return InvalidCheckin("request body cannot contain credentials; configure them on the account")
		}
	}
	if len(r.Headers) > 32 {
		return InvalidCheckin("too many headers")
	}
	for _, h := range r.Headers {
		if h.Name == "" || len(h.Name) > 128 || len(h.Value) > 4096 || strings.ContainsAny(h.Name+h.Value, "\r\n\x00") {
			return InvalidCheckin("invalid request header")
		}
		for _, c := range h.Name {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
				return InvalidCheckin("invalid header name")
			}
		}
		switch strings.ToLower(h.Name) {
		case "authorization", "cookie", "set-cookie", "new-api-user", "proxy-authorization", "x-api-key", "api-key", "token", "x-auth-token", "x-access-token", "x-refresh-token", "host", "content-length", "connection", "transfer-encoding", "upgrade":
			return InvalidCheckin("identity and transport headers cannot be configured on a site")
		}
	}
	return nil
}

func NormalizeCheckinSite(c *CheckinSiteConfig) error {
	c.Name, c.BaseURL = strings.TrimSpace(c.Name), strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.Name == "" || len(c.Name) > 128 {
		return InvalidCheckin("site name is required (maximum 128 bytes)")
	}
	if err := ValidateCheckinURL(c.BaseURL, false); err != nil {
		return err
	}
	switch c.Adapter {
	case "http", "new-api", "one-api", "one-hub", "done-hub":
	default:
		return InvalidCheckin("unsupported checkin adapter")
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return InvalidCheckin("invalid IANA timezone")
	}
	if c.WindowStart == "" {
		c.WindowStart = "00:00"
	}
	if c.WindowEnd == "" {
		c.WindowEnd = "24:00"
	}
	start, e1 := CheckinClock(c.WindowStart)
	end, e2 := CheckinClock(c.WindowEnd)
	if e1 != nil || e2 != nil || start >= end || start == 1440 {
		return InvalidCheckin("window must have start < end within the same day")
	}
	if c.JitterMinutes < 0 || c.JitterMinutes > 1440 {
		return InvalidCheckin("jitter must be between 0 and 1440 minutes")
	}
	if c.ProxyURL != "" {
		if err := ValidateCheckinURL(c.ProxyURL, true); err != nil {
			return err
		}
	}
	if c.ManualURL != "" {
		u, err := url.Parse(c.ManualURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return InvalidCheckin("invalid manual checkin URL")
		}
	}
	if c.Submit.Method == "" {
		c.Submit.Method = "POST"
	}
	if c.Submit.Path == "" && c.Adapter != "http" {
		c.Submit.Path = "/api/user/checkin"
	}
	if c.Submit.Headers == nil {
		c.Submit.Headers = []CheckinHeader{}
	}
	if err := ValidateCheckinRequest(c.Submit); err != nil {
		return err
	}
	for _, match := range []*CheckinMatch{c.Success, c.Already} {
		if match != nil {
			if err := ValidateCheckinMatch(*match); err != nil {
				return err
			}
		}
	}
	if c.Status != nil {
		if c.Status.Request.Method == "" {
			c.Status.Request.Method = "GET"
		}
		if c.Status.Request.Headers == nil {
			c.Status.Request.Headers = []CheckinHeader{}
		}
		if err := ValidateCheckinRequest(c.Status.Request); err != nil {
			return err
		}
		if err := ValidateCheckinMatch(c.Status.Checked); err != nil {
			return err
		}
		if err := ValidateCheckinMatch(c.Status.Unchecked); err != nil {
			return err
		}
		if c.Status.Checked.Path == c.Status.Unchecked.Path && string(c.Status.Checked.Value) == string(c.Status.Unchecked.Value) {
			return InvalidCheckin("checked and unchecked conditions must differ")
		}
		if c.Status.DatePath != "" {
			if err := ValidateCheckinMatch(CheckinMatch{Path: c.Status.DatePath, Value: json.RawMessage(`"date"`)}); err != nil {
				return err
			}
		}
	}
	if c.Reward.Source == "" {
		c.Reward.Source = "submit"
	}
	if c.Reward.Source != "submit" && c.Reward.Source != "status" {
		return InvalidCheckin("reward source must be submit or status")
	}
	if c.Reward.Source == "status" && c.Status == nil {
		return InvalidCheckin("status rewards require a status query")
	}
	if c.Reward.Path == "" {
		c.Reward.Path = "data.reward"
	}
	if err := ValidateCheckinMatch(CheckinMatch{Path: c.Reward.Path, Value: json.RawMessage(`0`)}); err != nil {
		return err
	}
	if c.Reward.Unit == "" {
		c.Reward.Unit = "points"
	}
	if len(c.Reward.Unit) > 32 || strings.ContainsAny(c.Reward.Unit, "\r\n\x00") {
		return InvalidCheckin("invalid reward unit")
	}
	if c.Reward.Multiplier == "" {
		c.Reward.Multiplier = "1"
	}
	if len(c.Reward.Multiplier) > 32 {
		return InvalidCheckin("invalid reward multiplier")
	}
	m, err := strconv.ParseFloat(c.Reward.Multiplier, 64)
	if err != nil || !(m > 0 && m <= 1e12) {
		return InvalidCheckin("reward multiplier must be positive and finite")
	}
	return nil
}

func containsCheckinSecretField(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(key))
			switch normalized {
			case "authorization", "cookie", "password", "secret", "client_secret", "token", "access_token", "refresh_token", "api_key", "apikey":
				return true
			}
			if containsCheckinSecretField(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsCheckinSecretField(child) {
				return true
			}
		}
	}
	return false
}

func ValidateCheckinCredentials(c CheckinCredentials) error {
	if strings.ContainsAny(c.Cookie+c.AccessToken+c.UserID, "\r\n\x00") || len(c.Cookie) > 16384 || len(c.AccessToken) > 16384 || len(c.UserID) > 64 {
		return InvalidCheckin("invalid credential length or control characters")
	}
	if c.Type == "cookie" && strings.TrimSpace(c.Cookie) != "" && c.AccessToken == "" {
		return nil
	}
	if c.Type == "bearer" && strings.TrimSpace(c.AccessToken) != "" && c.Cookie == "" {
		return nil
	}
	return InvalidCheckin("provide either a Cookie or a website Access Token")
}

func (s CheckinSettings) Validate() error {
	if s.Concurrency < 1 || s.Concurrency > 8 || s.SiteIntervalSeconds < 0 || s.SiteIntervalSeconds > 60 || s.HTTPTimeoutSeconds < 1 || s.HTTPTimeoutSeconds > 60 || s.ConfirmAttempts < 1 || s.ConfirmAttempts > 5 || s.ConfirmIntervalSeconds < 1 || s.ConfirmIntervalSeconds > 10 || s.ConfirmTimeoutMinutes < 1 || s.ConfirmTimeoutMinutes > 1440 {
		return InvalidCheckin("invalid checkin settings limits")
	}
	return nil
}
