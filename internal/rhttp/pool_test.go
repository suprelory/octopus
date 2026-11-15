package rhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestProxyClientsUseAuthenticationAndPreserveActiveStreams(t *testing.T) {
	t.Setenv("OCTOPUS_PROXY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32)))
	if err := db.InitDB("sqlite", filepath.ToSlash(filepath.Join(t.TempDir(), "pool.db")), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ClearProxyClients(); _ = db.Close() })
	ctx := context.Background()
	release := make(chan struct{})
	var releaseOnce sync.Once
	proxyA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.IsAbs() || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:proxy-secret")) {
			t.Error("HTTP proxy authentication or absolute request target missing")
		}
		_, _ = io.WriteString(w, "first-")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "last")
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); proxyA.Close() }()
	proxyB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "second-proxy") }))
	defer proxyB.Close()
	u, _ := url.Parse(proxyA.URL)
	u.User = url.UserPassword("alice", "proxy-secret")
	node, err := op.ProxySave(ctx, db.GetDB(), 0, model.ProxyWrite{Name: "Stream proxy", URL: u.String(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	selection := model.ProxySelection{Mode: "pool", ConfigID: &node.ID}
	client, err := ForSelection(ctx, selection)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := ForSelection(ctx, selection)
	if client != again {
		t.Fatal("identical proxy did not reuse client")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://upstream.invalid/stream", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	node, err = op.ProxySave(ctx, db.GetDB(), node.ID, model.ProxyWrite{Name: node.Name, URL: proxyB.URL, Enabled: true, Revision: node.Revision, ClearAuth: true})
	if err != nil {
		t.Fatal(err)
	}
	next, err := ForSelection(ctx, selection)
	if err != nil || next == client {
		t.Fatal("edited proxy reused old client", err)
	}
	releaseOnce.Do(func() { close(release) })
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "first-last" {
		t.Fatal("cache invalidation interrupted active stream", err)
	}
	second, err := next.Get("http://upstream.invalid/test")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(second.Body)
	second.Body.Close()
	if err != nil || string(body) != "second-proxy" {
		t.Fatal("new request did not use edited proxy", err)
	}
	_, err = op.ProxySave(ctx, db.GetDB(), node.ID, model.ProxyWrite{Name: node.Name, URL: node.URL, Revision: node.Revision, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ForSelection(ctx, selection); err == nil {
		t.Fatal("disabled node reused a cached client")
	}
}

func TestSOCKSAuthenticationAndContextCancellation(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			result := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					result <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				r := bufio.NewReader(conn)
				header := make([]byte, 2)
				if _, err := io.ReadFull(r, header); err != nil {
					result <- err
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err := io.ReadFull(r, methods); err != nil {
					result <- err
					return
				}
				_, _ = conn.Write([]byte{5, 2})
				if _, err := io.ReadFull(r, header); err != nil {
					result <- err
					return
				}
				username := make([]byte, int(header[1]))
				_, _ = io.ReadFull(r, username)
				passwordLength, _ := r.ReadByte()
				password := make([]byte, int(passwordLength))
				_, _ = io.ReadFull(r, password)
				if string(username) != "alice" || string(password) != "socks-secret" {
					result <- fmt.Errorf("SOCKS credentials missing")
					return
				}
				_, _ = conn.Write([]byte{1, 0})
				connect := make([]byte, 4)
				if _, err := io.ReadFull(r, connect); err != nil {
					result <- err
					return
				}
				if connect[3] != 3 {
					result <- fmt.Errorf("expected remote DNS target")
					return
				}
				size, _ := r.ReadByte()
				target := make([]byte, int(size)+2)
				_, _ = io.ReadFull(r, target)
				if string(target[:len(target)-2]) != "upstream.invalid" {
					result <- fmt.Errorf("wrong SOCKS target")
					return
				}
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
				request, err := http.ReadRequest(r)
				if err != nil {
					result <- err
					return
				}
				request.Body.Close()
				_, err = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nsocks")
				result <- err
			}()
			client, err := New(scheme + "://alice:socks-secret@" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			client.Timeout = 3 * time.Second
			response, err := client.Get("http://upstream.invalid/verify")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || string(body) != "socks" {
				t.Fatal("SOCKS request failed", err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.Copy(io.Discard, conn)
	}()
	client, err := New("socks5://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://upstream.invalid", nil)
	start := time.Now()
	_, err = client.Do(request)
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("SOCKS handshake ignored cancellation", err)
	}
	<-done
}

func TestProxyCacheBoundAndProbeValidation(t *testing.T) {
	ClearProxyClients()
	defer ClearProxyClients()
	for i := 0; i < proxyClientLimit+10; i++ {
		if _, err := ForResolved(model.ResolvedProxy{URL: fmt.Sprintf("http://127.0.0.1:%d", 10000+i)}); err != nil {
			t.Fatal(err)
		}
	}
	poolClients.Lock()
	size := poolClients.lru.Len()
	poolClients.Unlock()
	if size != proxyClientLimit {
		t.Fatal("unbounded proxy client cache", size)
	}
	for _, value := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::ffff:127.0.0.1", "fc00::1"} {
		if publicProbeIP(net.ParseIP(value)) {
			t.Fatal("private probe IP allowed", value)
		}
	}
	for _, target := range []string{"http://example.com", "https://127.0.0.1", "https://[::1]", "https://user:private@example.com"} {
		if _, err := TestProxy(context.Background(), "http://127.0.0.1:7890", target); err == nil {
			t.Fatal("unsafe probe target allowed", target)
		}
	}
	if _, err := New("http://alice:do-not-log@127.0.0.1:bad"); err == nil || strings.Contains(err.Error(), "do-not-log") {
		t.Fatal("proxy parse error leaked credentials", err)
	}
}
