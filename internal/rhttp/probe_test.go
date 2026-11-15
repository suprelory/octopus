package rhttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyProbeTLSAuthenticationAndHTTPResults(t *testing.T) {
	// Use a proxy certificate that is deliberately invalid for example.com.
	// The proxy and target must each be authenticated against their own name.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	proxyCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("probe lost target identity or exposed proxy authentication")
		}
		status, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if status == http.StatusFound {
			w.Header().Set("Location", "https://127.0.0.1/redirect-must-not-run")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, strings.Repeat("x", 128*1024))
	}))
	defer target.Close()
	roots := x509.NewCertPool()
	roots.AddCert(proxyCert)
	roots.AddCert(target.Certificate())
	previous := http.DefaultTransport
	transport := previous.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous; transport.CloseIdleConnections() }()
	lookup := func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host != "example.com" {
			t.Error("unexpected probe hostname", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			var tunnels sync.WaitGroup
			proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tunnels.Add(1)
				defer tunnels.Done()
				if r.Method != http.MethodConnect || r.Host != "8.8.8.8:443" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:probe-secret")) {
					t.Error("probe did not pin CONNECT destination or authenticate to proxy")
					http.Error(w, "invalid tunnel", http.StatusBadGateway)
					return
				}
				upstream, err := net.DialTimeout("tcp", strings.TrimPrefix(target.URL, "https://"), time.Second)
				if err != nil {
					t.Error(err)
					http.Error(w, "unavailable", http.StatusBadGateway)
					return
				}
				defer upstream.Close()
				client, buffer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer client.Close()
				_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
				_ = client.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				_ = buffer.Flush()
				done := make(chan struct{})
				go func() { defer close(done); _, _ = io.Copy(upstream, buffer); _ = upstream.Close() }()
				_, _ = io.Copy(client, upstream)
				_ = client.Close()
				<-done
			}))
			if scheme == "https" {
				proxy.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
				proxy.StartTLS()
			} else {
				proxy.Start()
			}
			defer func() { proxy.Close(); tunnels.Wait() }()
			endpoint, _ := url.Parse(proxy.URL)
			endpoint.User = url.UserPassword("alice", "probe-secret")
			for _, status := range []int{200, 401, 403, 302, 407} {
				before := requests.Load()
				result, err := testProxy(context.Background(), endpoint.String(), "https://example.com/"+strconv.Itoa(status), lookup)
				if err != nil || result.HTTPStatus != status || result.Connected != (status != 407) || requests.Load() != before+1 {
					t.Fatalf("status %d: result %+v, requests %d, error %v", status, result, requests.Load()-before, err)
				}
			}
		})
	}
}
