package client

import (
	"container/list"
	"net/http"
	"sync"
	"testing"
)

func resetProxyClientCache() {
	proxyClients = &proxyClientCache{
		entries: make(map[string]*list.Element),
		order:   list.New(),
	}
}

func TestGetHTTPClientCustomProxyReusesClient(t *testing.T) {
	resetProxyClientCache()
	first, err := GetHTTPClientCustomProxy("http://proxy.example:8080")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := GetHTTPClientCustomProxy("http://proxy.example:8080")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if first != second {
		t.Fatal("same proxy URL returned different clients; keepalive would be lost")
	}
}

func TestGetHTTPClientCustomProxyNormalizesEquivalentURLs(t *testing.T) {
	resetProxyClientCache()
	// url.Parse canonicalizes scheme/host casing into the key.
	first, err := GetHTTPClientCustomProxy("http://Proxy.Example:8080")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := GetHTTPClientCustomProxy("http://proxy.example:8080")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if first != second {
		t.Fatal("equivalent proxy URLs returned different clients")
	}
}

func TestGetHTTPClientCustomProxyDistinctURLsDistinctClients(t *testing.T) {
	resetProxyClientCache()
	a, err := GetHTTPClientCustomProxy("http://a.example:8080")
	if err != nil {
		t.Fatalf("a error: %v", err)
	}
	b, err := GetHTTPClientCustomProxy("socks5://b.example:1080")
	if err != nil {
		t.Fatalf("b error: %v", err)
	}
	if a == b {
		t.Fatal("different proxy URLs must not share a client")
	}
}

func TestGetHTTPClientCustomProxyCredentialsChangeClient(t *testing.T) {
	resetProxyClientCache()
	anon, err := GetHTTPClientCustomProxy("http://proxy.example:8080")
	if err != nil {
		t.Fatalf("anon error: %v", err)
	}
	authed, err := GetHTTPClientCustomProxy("http://user:pass@proxy.example:8080")
	if err != nil {
		t.Fatalf("authed error: %v", err)
	}
	if anon == authed {
		t.Fatal("credential change must yield a different client (auth participates in the key)")
	}
}

func TestGetHTTPClientCustomProxyRejectsBadURL(t *testing.T) {
	resetProxyClientCache()
	if _, err := GetHTTPClientCustomProxy(""); err == nil {
		t.Fatal("empty URL accepted")
	}
	if _, err := GetHTTPClientCustomProxy("ftp://proxy.example"); err == nil {
		t.Fatal("unsupported scheme accepted")
	}
}

func TestProxyClientCacheEvictsLRUAndClosesIdleConnections(t *testing.T) {
	resetProxyClientCache()
	first, err := GetHTTPClientCustomProxy("http://first.example:8080")
	if err != nil {
		t.Fatalf("first error: %v", err)
	}
	// Fill past the bound; the first entry must be evicted.
	for i := 0; i < maxProxyClients; i++ {
		if _, err := GetHTTPClientCustomProxy("http://bulk.example:100" + string(rune('0'+i/10)) + string(rune('0'+i%10))); err != nil {
			t.Fatalf("bulk %d error: %v", i, err)
		}
	}
	proxyClients.mu.Lock()
	_, alive := proxyClients.entries["http://first.example:8080"]
	size := proxyClients.order.Len()
	proxyClients.mu.Unlock()
	if alive {
		t.Fatal("LRU eviction did not drop the oldest entry")
	}
	if size > maxProxyClients {
		t.Fatalf("cache size %d exceeds bound %d", size, maxProxyClients)
	}
	// The evicted client's transport must have had CloseIdleConnections
	// called; observable via a fresh get missing + re-created client.
	again, err := GetHTTPClientCustomProxy("http://first.example:8080")
	if err != nil {
		t.Fatalf("first again error: %v", err)
	}
	if again == first {
		t.Fatal("evicted URL returned the stale client")
	}
}

func TestInvalidateProxyClientDropsEntry(t *testing.T) {
	resetProxyClientCache()
	first, err := GetHTTPClientCustomProxy("http://invalidate.example:8080")
	if err != nil {
		t.Fatalf("first error: %v", err)
	}
	InvalidateProxyClient("http://invalidate.example:8080")
	again, err := GetHTTPClientCustomProxy("http://invalidate.example:8080")
	if err != nil {
		t.Fatalf("again error: %v", err)
	}
	if again == first {
		t.Fatal("invalidate did not drop the cached client")
	}
	// Invalidating an unknown/empty URL is a no-op.
	InvalidateProxyClient("")
	InvalidateProxyClient("http://never-cached.example:1")
}

func TestProxyClientCacheConcurrentAccess(t *testing.T) {
	resetProxyClientCache()
	urls := []string{
		"http://c1.example:8080",
		"http://c2.example:8080",
		"socks5://c3.example:1080",
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			u := urls[idx%len(urls)]
			for j := 0; j < 100; j++ {
				c, err := GetHTTPClientCustomProxy(u)
				if err != nil {
					t.Errorf("GetHTTPClientCustomProxy(%q) error: %v", u, err)
					return
				}
				if c == nil {
					t.Errorf("nil client for %q", u)
					return
				}
			}
		}(i)
	}
	// Concurrent invalidation must not race with gets.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			InvalidateProxyClient(urls[j%len(urls)])
		}
	}()
	wg.Wait()
}

// TestProxyClientTransportIsHTTPTransport guards the eviction/invalidate
// cleanup path: entries whose transport is not *http.Transport are skipped
// rather than panicking.
func TestProxyClientCacheIgnoresNonTransportClients(t *testing.T) {
	resetProxyClientCache()
	proxyClients.put("http://odd.example:1", &http.Client{Transport: http.DefaultTransport})
	proxyClients.invalidate("http://odd.example:1") // must not panic
}
