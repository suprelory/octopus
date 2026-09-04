package compat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/cache"
)

const (
	geminiThoughtSignatureTTL             = 24 * time.Hour
	geminiThoughtSignatureCacheShards     = 64
	geminiThoughtSignatureCleanupInterval = time.Minute
	// The limit counts physical exact/fallback keys; a named tool call normally uses two.
	geminiThoughtSignatureMaxCacheEntries = 4096
)

type geminiThoughtSignatureEntry struct {
	signature string
	expiresAt time.Time
}

var (
	geminiThoughtSignatureCache       = cache.New[string, geminiThoughtSignatureEntry](geminiThoughtSignatureCacheShards)
	geminiThoughtSignatureCacheMu     sync.Mutex
	geminiThoughtSignatureLastCleanup time.Time
)

// GeminiSignatureScope isolates cached provider signatures across the scope
// dimensions supplied by the caller. When Gemini supplies a response ID or
// thought signature, generated fallback tool-call IDs also carry its digest.
// Values are hashed before they become a cache key, so identifiers are not
// retained.
type GeminiSignatureScope struct {
	TenantID  string
	APIKeyID  string
	SessionID string
	Model     string
	ChannelID string
	Format    string
}

type geminiSignatureScopeContextKey struct{}

func WithGeminiSignatureScope(ctx context.Context, scope GeminiSignatureScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, geminiSignatureScopeContextKey{}, scope)
}

func GeminiSignatureScopeFromContext(ctx context.Context) GeminiSignatureScope {
	if ctx == nil {
		return GeminiSignatureScope{}
	}
	scope, _ := ctx.Value(geminiSignatureScopeContextKey{}).(GeminiSignatureScope)
	return scope
}

// SaveGeminiThoughtSignatureScoped stores Gemini's opaque thoughtSignature in
// an isolation scope. New internal callers should use this form.
func SaveGeminiThoughtSignatureScoped(scope GeminiSignatureScope, toolCallID, toolName, signature string) {
	saveGeminiThoughtSignatureScopedAt(scope, toolCallID, toolName, signature, time.Now())
}

func saveGeminiThoughtSignatureScopedAt(scope GeminiSignatureScope, toolCallID, toolName, signature string, now time.Time) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" || strings.TrimSpace(signature) == "" {
		return
	}

	entry := geminiThoughtSignatureEntry{
		signature: signature,
		expiresAt: now.Add(geminiThoughtSignatureTTL),
	}
	keys := geminiThoughtSignatureKeys(scope, toolCallID, toolName)

	geminiThoughtSignatureCacheMu.Lock()
	defer geminiThoughtSignatureCacheMu.Unlock()

	protected := make(map[string]struct{}, len(keys))
	additionalEntries := 0
	for _, key := range keys {
		protected[key] = struct{}{}
		if _, exists := geminiThoughtSignatureCache.Get(key); !exists {
			additionalEntries++
		}
	}

	entryCount := geminiThoughtSignatureCache.Len()
	cleanupDue := geminiThoughtSignatureLastCleanup.IsZero() || now.Before(geminiThoughtSignatureLastCleanup) || now.Sub(geminiThoughtSignatureLastCleanup) >= geminiThoughtSignatureCleanupInterval
	var entries map[string]geminiThoughtSignatureEntry
	if cleanupDue || entryCount+additionalEntries > geminiThoughtSignatureMaxCacheEntries {
		entries = removeExpiredGeminiThoughtSignaturesLocked(now)
		entryCount = len(entries)
		geminiThoughtSignatureLastCleanup = now
	}
	if excess := entryCount + additionalEntries - geminiThoughtSignatureMaxCacheEntries; excess > 0 {
		if entries == nil {
			entries = geminiThoughtSignatureCache.GetAll()
		}
		evictOldestGeminiThoughtSignaturesLocked(entries, protected, excess)
	}
	for _, key := range keys {
		geminiThoughtSignatureCache.Set(key, entry)
	}
}

// RestoreGeminiThoughtSignatureScoped only returns signatures stored in the
// exact same isolation scope.
func RestoreGeminiThoughtSignatureScoped(scope GeminiSignatureScope, toolCallID, toolName string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}

	geminiThoughtSignatureCacheMu.Lock()
	defer geminiThoughtSignatureCacheMu.Unlock()

	now := time.Now()
	for _, key := range geminiThoughtSignatureKeys(scope, toolCallID, toolName) {
		entry, ok := geminiThoughtSignatureCache.Get(key)
		if !ok {
			continue
		}
		if !entry.expiresAt.After(now) {
			geminiThoughtSignatureCache.Del(key)
			continue
		}
		return entry.signature
	}
	return ""
}

func geminiThoughtSignatureKeys(scope GeminiSignatureScope, toolCallID, toolName string) []string {
	exactKey := geminiThoughtSignatureKey(scope, toolCallID, toolName)
	fallbackKey := geminiThoughtSignatureKey(scope, toolCallID, "")
	if exactKey == fallbackKey {
		return []string{exactKey}
	}
	return []string{exactKey, fallbackKey}
}

func removeExpiredGeminiThoughtSignaturesLocked(now time.Time) map[string]geminiThoughtSignatureEntry {
	entries := geminiThoughtSignatureCache.GetAll()
	for key, entry := range entries {
		if entry.expiresAt.After(now) {
			continue
		}
		geminiThoughtSignatureCache.Del(key)
		delete(entries, key)
	}
	return entries
}

func evictOldestGeminiThoughtSignaturesLocked(entries map[string]geminiThoughtSignatureEntry, protected map[string]struct{}, count int) {
	for count > 0 {
		oldestKey := ""
		var oldestExpiry time.Time
		for key, entry := range entries {
			if _, keep := protected[key]; keep {
				continue
			}
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) || (entry.expiresAt.Equal(oldestExpiry) && key < oldestKey) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
			}
		}
		if oldestKey == "" {
			return
		}
		geminiThoughtSignatureCache.Del(oldestKey)
		delete(entries, oldestKey)
		count--
	}
}

func geminiThoughtSignatureKey(scope GeminiSignatureScope, toolCallID, toolName string) string {
	parts := []string{
		"v2",
		strings.TrimSpace(scope.TenantID),
		strings.TrimSpace(scope.APIKeyID),
		strings.TrimSpace(scope.SessionID),
		strings.TrimSpace(scope.Model),
		strings.TrimSpace(scope.ChannelID),
		strings.TrimSpace(scope.Format),
		strings.TrimSpace(toolCallID),
		strings.TrimSpace(toolName),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
