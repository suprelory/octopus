package clientip

import (
	"net/http"
	"testing"
)

func TestParseTrustedProxiesCanonicalizesAndDeduplicates(t *testing.T) {
	resolver, normalized, err := ParseTrustedProxies("172.24.0.1, 10.0.0.0/24\n172.24.0.1")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	if normalized != "10.0.0.0/24,172.24.0.1" {
		t.Fatalf("normalized trusted proxies = %q", normalized)
	}
	if resolver.Count() != 2 {
		t.Fatalf("trusted proxy count = %d, want 2", resolver.Count())
	}
}

func TestResolveIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	resolver, _, err := ParseTrustedProxies("10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("X-Forwarded-For", "203.0.113.7")
	if got := resolver.Resolve("192.0.2.10:1234", headers); got != "192.0.2.10" {
		t.Fatalf("resolved IP = %q, want TCP peer", got)
	}
}

func TestResolveUsesUntrustedClientFromForwardedChain(t *testing.T) {
	resolver, _, err := ParseTrustedProxies("10.0.0.1,10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.2")
	if got := resolver.Resolve("10.0.0.1:1234", headers); got != "203.0.113.7" {
		t.Fatalf("resolved IP = %q, want forwarded client", got)
	}
}

func TestResolveFallsBackToRealIPHeader(t *testing.T) {
	resolver, _, err := ParseTrustedProxies("10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	headers := make(http.Header)
	headers.Set("X-Real-IP", "203.0.113.8")
	if got := resolver.Resolve("10.0.0.1:1234", headers); got != "203.0.113.8" {
		t.Fatalf("resolved IP = %q, want X-Real-IP", got)
	}
}

func TestParseTrustedProxiesRejectsInvalidEntry(t *testing.T) {
	if _, _, err := ParseTrustedProxies("proxy.example.com"); err == nil {
		t.Fatal("expected hostname to be rejected")
	}
}

func TestParseTrustedProxiesDetectsCatchAllPrefix(t *testing.T) {
	resolver, _, err := ParseTrustedProxies("0.0.0.0/0")
	if err != nil {
		t.Fatal(err)
	}
	if !resolver.TrustsAll() {
		t.Fatal("expected catch-all prefix to be marked unsafe")
	}
}
