package checkin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/rhttp"
)

type HTTPResult struct {
	StatusCode          int
	Body                []byte
	Err                 error
	NetworkReason       string
	ContentType         string
	Cookies             []*http.Cookie
	URL                 *url.URL
	CloudflareChallenge bool
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
	return doRequestHTTP(ctx, client, site, config, credentials, date, true)
}

func doRequestHTTP(ctx context.Context, client *http.Client, site model.CheckinSiteConfig, config model.CheckinRequest, credentials model.CheckinCredentials, date string, expand bool) HTTPResult {
	return doRequestHTTPWithToken(ctx, client, site, config, credentials, date, expand, nil)
}

func doRequestHTTPWithToken(ctx context.Context, client *http.Client, site model.CheckinSiteConfig, config model.CheckinRequest, credentials model.CheckinCredentials, date string, expand bool, token *requestToken) HTTPResult {
	base, err := checkinRequestURL(site.BaseURL, config.Path, date)
	if err != nil {
		return HTTPResult{Err: errors.New("invalid site URL")}
	}
	body := config.Body
	if expand {
		for key, value := range map[string]string{"{{checkin_date}}": date, "{{user_id}}": credentials.UserID} {
			encoded, _ := json.Marshal(value)
			body = strings.ReplaceAll(body, key, string(encoded[1:len(encoded)-1]))
		}
	}
	if body != "" && !json.Valid([]byte(body)) {
		return HTTPResult{Err: errors.New("invalid expanded JSON body")}
	}
	if token != nil {
		switch token.location {
		case "query":
			query := base.Query()
			query.Set(token.field, token.value)
			base.RawQuery = query.Encode()
		case "json":
			object := map[string]json.RawMessage{}
			if body != "" && (json.Unmarshal([]byte(body), &object) != nil || object == nil) {
				return HTTPResult{Err: errors.New("Turnstile requires a JSON object body")}
			}
			object[token.field], _ = json.Marshal(token.value)
			encoded, _ := json.Marshal(object)
			body = string(encoded)
		}
	}
	req, err := http.NewRequestWithContext(ctx, config.Method, base.String(), bytes.NewBufferString(body))
	if err != nil {
		return HTTPResult{Err: errors.New("invalid checkin request")}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Octopus-Checkin/1.0")
	if site.Adapter == "anyrouter" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("User-Agent", anyRouterUserAgent)
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		if baseURL, parseErr := url.Parse(site.BaseURL); parseErr == nil {
			req.Header.Set("Origin", baseURL.Scheme+"://"+baseURL.Host)
			req.Header.Set("Referer", strings.TrimRight(site.BaseURL, "/")+"/console")
		}
	}
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
		if site.Adapter == "anyrouter" {
			for _, name := range []string{"Veloera-User", "voapi-user", "User-id", "Rix-Api-User", "neo-api-user"} {
				req.Header.Set(name, credentials.UserID)
			}
		}
	}
	if token != nil && token.location == "header" {
		req.Header.Set(token.field, token.value)
	}
	if token != nil && len(token.cookies) > 0 {
		// Keep cookies issued with the refusal only for this same-origin retry;
		// cookiejar enforces Domain, Path, Secure and deletion/expiry semantics.
		jar, _ := cookiejar.New(nil)
		jar.SetCookies(base, req.Cookies())
		for _, cookie := range token.cookies {
			// Some Go versions treat loopback HTTP as a secure context; keep the
			// same strict HTTPS requirement used by persisted router sessions.
			if cookie.Secure && base.Scheme != "https" {
				continue
			}
			jar.SetCookies(base, []*http.Cookie{cookie})
		}
		req.Header.Del("Cookie")
		for _, cookie := range jar.Cookies(base) {
			req.AddCookie(cookie)
		}
	}
	response, err := client.Do(req)
	if err != nil {
		return HTTPResult{Err: errors.New("checkin request failed or timed out"), NetworkReason: classifyNetworkError(err)}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if len(raw) > 1<<20 {
		return HTTPResult{StatusCode: response.StatusCode, Cookies: response.Cookies(), URL: base, ContentType: response.Header.Get("Content-Type"), Err: errors.New("checkin response exceeded its limit"), NetworkReason: model.CheckinNetworkResponseTooLarge}
	}
	if err != nil {
		reason := classifyNetworkError(err)
		if reason == model.CheckinNetworkError || reason == model.CheckinNetworkConnectionError {
			reason = model.CheckinNetworkResponseReadError
		}
		return HTTPResult{StatusCode: response.StatusCode, Cookies: response.Cookies(), URL: base, ContentType: response.Header.Get("Content-Type"), Err: errors.New("checkin response could not be read"), NetworkReason: reason}
	}
	return HTTPResult{StatusCode: response.StatusCode, Body: raw, Cookies: response.Cookies(), URL: base, ContentType: response.Header.Get("Content-Type"), CloudflareChallenge: strings.EqualFold(response.Header.Get("Cf-Mitigated"), "challenge")}
}

func classifyNetworkError(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return model.CheckinNetworkDNSError
	}
	lowered := strings.ToLower(err.Error())
	var recordErr tls.RecordHeaderError
	var verifyErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	var hostnameError x509.HostnameError
	var rootsError x509.SystemRootsError
	if errors.As(err, &recordErr) || errors.As(err, &verifyErr) || errors.As(err, &unknownAuthority) || errors.As(err, &invalidCertificate) || errors.As(err, &hostnameError) || errors.As(err, &rootsError) || strings.Contains(lowered, "tls:") {
		return model.CheckinNetworkTLSError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.CheckinNetworkTimeout
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return model.CheckinNetworkTimeout
	}
	var opErr *net.OpError
	if (errors.As(err, &opErr) && strings.EqualFold(opErr.Op, "proxyconnect")) || strings.Contains(lowered, "proxyconnect") || strings.Contains(lowered, "proxy error") {
		return model.CheckinNetworkProxyError
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return model.CheckinNetworkConnectionRefused
	}
	if strings.Contains(lowered, "actively refused") || strings.Contains(lowered, "connection refused") {
		return model.CheckinNetworkConnectionRefused
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return model.CheckinNetworkConnectionReset
	}
	if strings.Contains(lowered, "connection reset") || strings.Contains(lowered, "forcibly closed by the remote host") {
		return model.CheckinNetworkConnectionReset
	}
	if errors.Is(err, context.Canceled) {
		return model.CheckinNetworkError
	}
	return model.CheckinNetworkConnectionError
}

func checkinRequestURL(baseURL, requestPath, date string) (*url.URL, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	path, err := url.ParseRequestURI(strings.ReplaceAll(requestPath, "{{checkin_date}}", date))
	if err != nil {
		return nil, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path.Path
	base.RawPath, base.RawQuery = "", path.RawQuery
	return base, nil
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

type Capability = model.CheckinPlatform

func Capabilities() []Capability { return model.CheckinPlatforms() }
