package compat

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSignatureStoreScopeAndInstanceIsolation(t *testing.T) {
	store := NewMemorySignatureStore()
	scope := GeminiSignatureScope{TenantID: "tenant", APIKeyID: "key", SessionID: "session", Model: "model", ChannelID: "channel", Format: "anthropic/messages"}
	store.Save(scope, "call", "lookup", "sig-a")
	if got := store.Restore(scope, "call", "lookup"); got != "sig-a" {
		t.Fatalf("same-scope restore = %q", got)
	}
	for index := range 6 {
		other := scope
		fields := []*string{&other.TenantID, &other.APIKeyID, &other.SessionID, &other.Model, &other.ChannelID, &other.Format}
		*fields[index] += "-other"
		if got := store.Restore(other, "call", "lookup"); got != "" {
			t.Fatalf("cross-scope restore for dimension %d = %q", index, got)
		}
	}
	if got := NewMemorySignatureStore().Restore(scope, "call", "lookup"); got != "" {
		t.Fatalf("cross-instance restore = %q", got)
	}
}

func TestSignatureStoreOpaqueBytesAndFallback(t *testing.T) {
	store := NewMemorySignatureStore()
	scope := GeminiSignatureScope{APIKeyID: "opaque"}
	signature := " \tsignature-bytes\n "
	store.Save(scope, "call", "lookup", signature)
	for _, name := range []string{"lookup", "", "renamed"} {
		if got := store.Restore(scope, "call", name); got != signature {
			t.Fatalf("signature changed for %q: %q", name, got)
		}
	}
	store.Save(scope, "call", "other", "second")
	if got := store.Restore(scope, "call", "lookup"); got != signature {
		t.Fatalf("exact match lost priority: %q", got)
	}
	if got := store.Restore(scope, "call", ""); got != "second" {
		t.Fatalf("fallback not refreshed: %q", got)
	}
}

func TestSignatureStoreEvictsOldestAtCapacity(t *testing.T) {
	now := time.Now()
	store := &MemorySignatureStore{now: func() time.Time { return now }}
	scope := GeminiSignatureScope{APIKeyID: "capacity"}
	logicalEntries := geminiThoughtSignatureMaxCacheEntries/2 + 2
	for index := range logicalEntries {
		store.Save(scope, fmt.Sprintf("call_%d", index), "lookup", fmt.Sprintf("sig-%d", index))
		now = now.Add(time.Second)
	}
	if len(store.entries) != geminiThoughtSignatureMaxCacheEntries {
		t.Fatalf("cache entries = %d", len(store.entries))
	}
	if got := store.Restore(scope, "call_0", "lookup"); got != "" {
		t.Fatalf("oldest signature was not evicted: %q", got)
	}
	last := logicalEntries - 1
	if got := store.Restore(scope, fmt.Sprintf("call_%d", last), "lookup"); got != fmt.Sprintf("sig-%d", last) {
		t.Fatalf("newest signature = %q", got)
	}
	store.Save(scope, fmt.Sprintf("call_%d", last), "lookup", "renewed")
	if len(store.entries) != geminiThoughtSignatureMaxCacheEntries {
		t.Fatalf("renewal changed capacity: %d", len(store.entries))
	}
}

func TestSignatureStoreExpiryAndCleanup(t *testing.T) {
	now := time.Now()
	store := &MemorySignatureStore{now: func() time.Time { return now }}
	scope := GeminiSignatureScope{}
	store.Save(scope, "expires", "lookup", "sig")
	now = now.Add(geminiThoughtSignatureTTL)
	if got := store.Restore(scope, "expires", "lookup"); got != "" {
		t.Fatalf("signature survived exact TTL boundary: %q", got)
	}
	store.Save(scope, "old", "lookup", "old")
	now = now.Add(geminiThoughtSignatureTTL + time.Hour)
	store.Save(scope, "live", "lookup", "live")
	if len(store.entries) != 2 || store.Restore(scope, "live", "lookup") != "live" {
		t.Fatalf("expired cleanup left %d keys", len(store.entries))
	}
}

func TestSignatureStoreConcurrent(t *testing.T) {
	store := NewMemorySignatureStore()
	var wg sync.WaitGroup
	for index := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scope := GeminiSignatureScope{APIKeyID: fmt.Sprint(index)}
			for range 16 {
				store.Save(scope, "same-call", "lookup", fmt.Sprint(index))
				if got := store.Restore(scope, "same-call", "lookup"); got != fmt.Sprint(index) {
					t.Errorf("signature crossed concurrent scopes: %q", got)
				}
			}
		}()
	}
	wg.Wait()
}
