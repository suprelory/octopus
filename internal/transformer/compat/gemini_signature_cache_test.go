package compat

import (
	"fmt"
	"testing"
	"time"
)

func resetGeminiThoughtSignatureCacheForTest(t *testing.T) {
	t.Helper()
	geminiThoughtSignatureCacheMu.Lock()
	geminiThoughtSignatureCache.Clear()
	geminiThoughtSignatureLastCleanup = time.Time{}
	geminiThoughtSignatureCacheMu.Unlock()
	t.Cleanup(func() {
		geminiThoughtSignatureCacheMu.Lock()
		geminiThoughtSignatureCache.Clear()
		geminiThoughtSignatureLastCleanup = time.Time{}
		geminiThoughtSignatureCacheMu.Unlock()
	})
}

func TestGeminiThoughtSignatureCacheIsScoped(t *testing.T) {
	resetGeminiThoughtSignatureCacheForTest(t)

	toolCallID := "call_scoped_signature"
	scopeA := GeminiSignatureScope{APIKeyID: "1", Model: "model-a", Format: "anthropic/messages"}
	scopeB := GeminiSignatureScope{APIKeyID: "2", Model: "model-a", Format: "anthropic/messages"}

	SaveGeminiThoughtSignatureScoped(scopeA, toolCallID, "lookup", "sig-a")

	if got := RestoreGeminiThoughtSignatureScoped(scopeA, toolCallID, "lookup"); got != "sig-a" {
		t.Fatalf("same-scope restore = %q, want sig-a", got)
	}
	if got := RestoreGeminiThoughtSignatureScoped(scopeB, toolCallID, "lookup"); got != "" {
		t.Fatalf("cross-scope restore = %q, want empty", got)
	}
}

func TestGeminiThoughtSignatureCachePreservesOpaqueBytes(t *testing.T) {
	resetGeminiThoughtSignatureCacheForTest(t)
	scope := GeminiSignatureScope{APIKeyID: "opaque", Model: "model-a", Format: "anthropic/messages"}
	signature := " \tsignature-bytes\n "

	SaveGeminiThoughtSignatureScoped(scope, "call_opaque", "lookup", signature)
	if got := RestoreGeminiThoughtSignatureScoped(scope, "call_opaque", "lookup"); got != signature {
		t.Fatalf("opaque signature changed: got %q, want %q", got, signature)
	}
}

func TestGeminiThoughtSignatureCacheEvictsOldestAtCapacity(t *testing.T) {
	resetGeminiThoughtSignatureCacheForTest(t)

	scope := GeminiSignatureScope{APIKeyID: "capacity", Model: "model-a", Format: "anthropic/messages"}
	baseTime := time.Now()
	logicalEntries := geminiThoughtSignatureMaxCacheEntries/2 + 2
	for index := 0; index < logicalEntries; index++ {
		toolCallID := fmt.Sprintf("call_capacity_%d", index)
		saveGeminiThoughtSignatureScopedAt(scope, toolCallID, "lookup", fmt.Sprintf("sig-%d", index), baseTime.Add(time.Duration(index)*time.Second))
	}

	geminiThoughtSignatureCacheMu.Lock()
	cacheEntries := geminiThoughtSignatureCache.Len()
	geminiThoughtSignatureCacheMu.Unlock()
	if cacheEntries != geminiThoughtSignatureMaxCacheEntries {
		t.Fatalf("cache entries = %d, want %d", cacheEntries, geminiThoughtSignatureMaxCacheEntries)
	}
	if got := RestoreGeminiThoughtSignatureScoped(scope, "call_capacity_0", "lookup"); got != "" {
		t.Fatalf("oldest signature was not evicted: %q", got)
	}
	lastIndex := logicalEntries - 1
	if got := RestoreGeminiThoughtSignatureScoped(scope, fmt.Sprintf("call_capacity_%d", lastIndex), "lookup"); got != fmt.Sprintf("sig-%d", lastIndex) {
		t.Fatalf("newest signature = %q, want sig-%d", got, lastIndex)
	}
}

func TestGeminiThoughtSignatureCacheCleansExpiredEntriesBeforeSave(t *testing.T) {
	resetGeminiThoughtSignatureCacheForTest(t)

	scope := GeminiSignatureScope{APIKeyID: "expiry", Model: "model-a", Format: "anthropic/messages"}
	now := time.Now()
	saveGeminiThoughtSignatureScopedAt(scope, "call_expired", "lookup", "expired", now.Add(-geminiThoughtSignatureTTL-time.Hour))
	saveGeminiThoughtSignatureScopedAt(scope, "call_live", "lookup", "live", now)

	geminiThoughtSignatureCacheMu.Lock()
	cacheEntries := geminiThoughtSignatureCache.Len()
	geminiThoughtSignatureCacheMu.Unlock()
	if cacheEntries != 2 {
		t.Fatalf("cache entries after expired cleanup = %d, want 2", cacheEntries)
	}
	if got := RestoreGeminiThoughtSignatureScoped(scope, "call_live", "lookup"); got != "live" {
		t.Fatalf("live signature = %q, want live", got)
	}
}
