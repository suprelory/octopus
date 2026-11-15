package checkin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/rhttp"
)

type HTTPResult struct {
	StatusCode int
	Body       []byte
	Err        error
}
type SubmitResult struct {
	Status, Reason string
	SafeToRetry    bool
}

func newHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	var transport *http.Transport
	if proxyURL != "" {
		client, err := rhttp.New(proxyURL)
		if err != nil {
			return nil, errors.New("invalid checkin proxy configuration")
		}
		var ok bool
		transport, ok = client.Transport.(*http.Transport)
		if !ok {
			return nil, errors.New("unsupported proxy transport")
		}
	} else {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, errors.New("unsupported HTTP transport")
		}
		transport = base.Clone()
		transport.Proxy = nil
	}
	// Even GET can submit a check-in. Fresh HTTP/1 connections prevent the
	// standard transport from silently replaying a request on a reused connection.
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func requestHTTP(ctx context.Context, client *http.Client, site model.CheckinSiteConfig, config model.CheckinRequest, credentials model.CheckinCredentials, date string) HTTPResult {
	base, err := url.Parse(site.BaseURL)
	if err != nil {
		return HTTPResult{Err: errors.New("invalid site URL")}
	}
	path, err := url.ParseRequestURI(strings.ReplaceAll(config.Path, "{{checkin_date}}", date))
	if err != nil {
		return HTTPResult{Err: errors.New("invalid request path")}
	}
	base.Path = strings.TrimRight(base.Path, "/") + path.Path
	base.RawPath = ""
	base.RawQuery = path.RawQuery
	body := config.Body
	for key, value := range map[string]string{"{{checkin_date}}": date, "{{user_id}}": credentials.UserID} {
		encoded, _ := json.Marshal(value)
		body = strings.ReplaceAll(body, key, string(encoded[1:len(encoded)-1]))
	}
	if body != "" && !json.Valid([]byte(body)) {
		return HTTPResult{Err: errors.New("invalid expanded JSON body")}
	}
	req, err := http.NewRequestWithContext(ctx, config.Method, base.String(), bytes.NewBufferString(body))
	if err != nil {
		return HTTPResult{Err: errors.New("invalid checkin request")}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Octopus-Checkin/1.0")
	if site.UserAgent != "" {
		req.Header.Set("User-Agent", site.UserAgent)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, header := range config.Headers {
		value := strings.NewReplacer("{{checkin_date}}", date, "{{user_id}}", credentials.UserID).Replace(header.Value)
		req.Header.Set(header.Name, value)
	}
	if credentials.Type == "cookie" {
		req.Header.Set("Cookie", credentials.Cookie)
	} else if credentials.Type == "bearer" {
		req.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	}
	if credentials.UserID != "" {
		req.Header.Set("New-Api-User", credentials.UserID)
	}
	response, err := client.Do(req)
	if err != nil {
		return HTTPResult{Err: errors.New("checkin request failed or timed out")}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return HTTPResult{StatusCode: response.StatusCode, Err: errors.New("checkin response could not be read within its limit")}
	}
	return HTTPResult{StatusCode: response.StatusCode, Body: raw}
}

func interpretSubmit(config model.CheckinSiteConfig, status int, body []byte) SubmitResult {
	if status == 404 || status == 405 {
		return SubmitResult{Status: "failed", Reason: "unsupported_endpoint", SafeToRetry: true}
	}
	data, err := decodeObject(body)
	if status == 401 || status == 403 {
		v, ok := field(data, "success")
		rejected, typed := v.(bool)
		return SubmitResult{Status: "manual_required", Reason: "invalid_credentials", SafeToRetry: ok && typed && !rejected}
	}
	if status == 429 {
		return SubmitResult{Status: "manual_required", Reason: "rate_limited"}
	}
	if err != nil {
		reason := "unconfirmed_response"
		if bytes.Contains(bytes.ToLower(body), []byte("cloudflare")) || bytes.Contains(bytes.ToLower(body), []byte("turnstile")) {
			reason = "verification_required"
		}
		return SubmitResult{Status: "manual_required", Reason: reason}
	}
	if (status >= 200 && status < 300) || status == 400 || status == 409 {
		for _, path := range []string{"message", "msg", "error.message"} {
			v, _ := field(data, path)
			message, _ := v.(string)
			lower := strings.ToLower(message)
			if strings.Contains(message, "今日已签到") || strings.Contains(message, "今天已经签到") || strings.Contains(lower, "already checked in") || strings.Contains(lower, "already signed in today") {
				return SubmitResult{Status: "already_checked_in", Reason: "already_checked_in"}
			}
		}
	}
	if status < 200 || status >= 300 {
		return SubmitResult{Status: "manual_required", Reason: "upstream_http_error"}
	}
	if config.Already != nil && matches(data, *config.Already) {
		return SubmitResult{Status: "already_checked_in", Reason: "already_checked_in"}
	}
	success := model.CheckinMatch{Path: "success", Value: json.RawMessage(`true`)}
	if config.Success != nil {
		success = *config.Success
	}
	if matches(data, success) {
		return SubmitResult{Status: "success", Reason: "checked_in"}
	}
	return SubmitResult{Status: "manual_required", Reason: "unconfirmed_response"}
}

type Capability struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	CredentialTypes []string `json:"credential_types"`
}

func Capabilities() []Capability {
	result := []Capability{}
	for _, platform := range []struct{ id, name string }{{"new-api", "New API"}, {"one-api", "One API"}, {"one-hub", "OneHub"}, {"done-hub", "DoneHub"}, {"sub2api", "Sub2API"}, {"http", "Custom HTTP"}} {
		path := "/api/user/checkin"
		if platform.id == "http" || platform.id == "sub2api" {
			path = ""
		}
		result = append(result, Capability{ID: platform.id, Name: platform.name, Path: path, CredentialTypes: []string{"cookie", "bearer"}})
	}
	return result
}
