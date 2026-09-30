package compat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	geminiThoughtSignatureTTL             = 24 * time.Hour
	geminiThoughtSignatureCleanupInterval = time.Minute
	// Physical exact/fallback keys; a named tool call normally uses two.
	geminiThoughtSignatureMaxCacheEntries = 4096
)

// GeminiSignatureScope isolates cached provider signatures across dimensions
// supplied by the host. MemorySignatureStore hashes the identifiers before use.
// Hosts should supply tenant/key/session scope for their isolation boundary.
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

// SignatureStore recovers opaque Gemini tool signatures across client turns.
// Implementations must be safe for concurrent use, preserve opaque bytes and
// isolate every scope dimension. An empty Restore result means no signature.
type SignatureStore interface {
	Save(scope GeminiSignatureScope, toolCallID, toolName, signature string)
	Restore(scope GeminiSignatureScope, toolCallID, toolName string) string
}

// NoopSignatureStore explicitly disables cross-request signature recovery.
type NoopSignatureStore struct{}

func (NoopSignatureStore) Save(GeminiSignatureScope, string, string, string)   {}
func (NoopSignatureStore) Restore(GeminiSignatureScope, string, string) string { return "" }

type geminiThoughtSignatureEntry struct {
	signature string
	expiresAt time.Time
}

// MemorySignatureStore is an instance-owned, bounded signature cache. Entries
// expire after 24 hours; at most 4096 exact/fallback keys are retained. Cleanup is
// lazy and eviction is deterministic (earliest expiry, then hashed key). It owns
// no goroutines. Its zero value is ready to use and must not be copied after use.
type MemorySignatureStore struct {
	mu          sync.Mutex
	entries     map[string]geminiThoughtSignatureEntry
	lastCleanup time.Time
	now         func() time.Time
}

func NewMemorySignatureStore() *MemorySignatureStore { return &MemorySignatureStore{} }

func (s *MemorySignatureStore) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *MemorySignatureStore) Save(scope GeminiSignatureScope, toolCallID, toolName, signature string) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" || strings.TrimSpace(signature) == "" {
		return
	}
	keys := geminiThoughtSignatureKeys(scope, toolCallID, toolName)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]geminiThoughtSignatureEntry)
	}
	now := s.currentTime()
	cleanupDue := s.lastCleanup.IsZero() || now.Before(s.lastCleanup) || now.Sub(s.lastCleanup) >= geminiThoughtSignatureCleanupInterval
	if cleanupDue || len(s.entries)+len(keys) > geminiThoughtSignatureMaxCacheEntries {
		for key, entry := range s.entries {
			if !entry.expiresAt.After(now) {
				delete(s.entries, key)
			}
		}
		s.lastCleanup = now
	}
	protected := make(map[string]struct{}, len(keys))
	additional := 0
	for _, key := range keys {
		protected[key] = struct{}{}
		if _, exists := s.entries[key]; !exists {
			additional++
		}
	}
	for len(s.entries)+additional > geminiThoughtSignatureMaxCacheEntries {
		oldestKey := ""
		var oldestExpiry time.Time
		for key, entry := range s.entries {
			if _, keep := protected[key]; keep {
				continue
			}
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) || (entry.expiresAt.Equal(oldestExpiry) && key < oldestKey) {
				oldestKey, oldestExpiry = key, entry.expiresAt
			}
		}
		delete(s.entries, oldestKey)
	}
	entry := geminiThoughtSignatureEntry{signature: signature, expiresAt: now.Add(geminiThoughtSignatureTTL)}
	for _, key := range keys {
		s.entries[key] = entry
	}
}

func (s *MemorySignatureStore) Restore(scope GeminiSignatureScope, toolCallID, toolName string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.currentTime()
	for _, key := range geminiThoughtSignatureKeys(scope, toolCallID, toolName) {
		entry, ok := s.entries[key]
		if !ok {
			continue
		}
		if !entry.expiresAt.After(now) {
			delete(s.entries, key)
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
