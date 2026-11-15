package rhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultProxyTestTarget = "https://api.openai.com/v1/models"

type ProxyTestResult struct {
	Connected  bool   `json:"connected"`
	HTTPStatus int    `json:"http_status"`
	DurationMS int64  `json:"duration_ms"`
	Message    string `json:"message"`
}

// Probes pin a public target IP before CONNECT/SOCKS and retain the original TLS
// name and Host header. HTTPS and no redirects prevent proxy-side DNS rebinding
// from turning this endpoint into an arbitrary internal HTTP request facility.
func TestProxy(ctx context.Context, proxyURL, target string) (ProxyTestResult, error) {
	return testProxy(ctx, proxyURL, target, net.DefaultResolver.LookupIPAddr)
}

func testProxy(ctx context.Context, proxyURL, target string, lookup func(context.Context, string) ([]net.IPAddr, error)) (ProxyTestResult, error) {
	result := ProxyTestResult{}
	if strings.TrimSpace(target) == "" {
		target = DefaultProxyTestTarget
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return result, errors.New("test target must be a public HTTPS URL without credentials or fragment")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	start := time.Now()
	ips, err := lookup(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return result, errors.New("test target could not be resolved")
	}
	for _, address := range ips {
		ip := address.IP
		if !publicProbeIP(ip) {
			return result, errors.New("test target must resolve to public addresses")
		}
	}
	client, err := New(proxyURL)
	if err != nil {
		return result, err
	}
	defer client.CloseIdleConnections()
	client.Timeout = 20 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := client.Transport.(*http.Transport)
	targetTLS := transport.TLSClientConfig
	if targetTLS == nil {
		targetTLS = &tls.Config{}
	} else {
		targetTLS = targetTLS.Clone()
	}
	targetTLS.MinVersion = max(targetTLS.MinVersion, tls.VersionTLS12)
	proxyEndpoint, _ := url.Parse(strings.TrimSpace(proxyURL)) // New has already validated the URL.
	if strings.EqualFold(proxyEndpoint.Scheme, "https") {
		// The outer TLS connection authenticates the proxy, while the inner
		// connection authenticates the target behind the pinned CONNECT address.
		proxyTLS := targetTLS.Clone()
		proxyTLS.ServerName = proxyEndpoint.Hostname()
		proxyTLS.NextProtos = []string{"http/1.1"}
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 20 * time.Second}, Config: proxyTLS}
		transport.DialTLSContext = dialer.DialContext
	}
	targetTLS.ServerName = u.Hostname()
	transport.TLSClientConfig = targetTLS
	host := u.Host
	port := u.Port()
	if port == "" {
		port = "443"
	}
	u.Host = net.JoinHostPort(ips[0].IP.String(), port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, errors.New("invalid test target")
	}
	req.Host = host
	req.Header.Set("User-Agent", "Octopus-Proxy-Test/1.0")
	response, requestErr := client.Do(req)
	result.DurationMS = time.Since(start).Milliseconds()
	if requestErr != nil {
		result.Message = "connection_failed"
		return result, nil
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	result.DurationMS = time.Since(start).Milliseconds()
	result.Connected = readErr == nil && response.StatusCode != http.StatusProxyAuthRequired
	result.Message = "http_response"
	if !result.Connected {
		result.Message = "connection_failed"
	}
	return result, nil
}

func publicProbeIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, network := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16"} {
		_, block, _ := net.ParseCIDR(network)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
