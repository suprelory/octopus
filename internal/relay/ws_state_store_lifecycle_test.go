package relay

import (
	"fmt"
	"testing"
	"time"
)

func wsStoreResidentCount() int {
	n := 0
	wsConversationStore.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// Keys embed a per-connection session ID, so expired entries are never revisited
// by a lookup. The sweeper is what reclaims them.
func TestSweepRemovesExpiredWSConversationStates(t *testing.T) {
	resetWSConversationStateStore()
	t.Cleanup(resetWSConversationStateStore)

	const conns = 500
	for i := 0; i < conns; i++ {
		storeWSConversationState(7, "gpt-4o", &wsConversationState{
			DownstreamSessionID: fmt.Sprintf("ws_%d", i),
			ChannelID:           1,
		}, time.Millisecond)
	}
	if got := wsStoreResidentCount(); got != conns {
		t.Fatalf("expected %d entries stored, got %d", conns, got)
	}

	time.Sleep(10 * time.Millisecond)
	sweepExpiredWSConversationStates()

	if got := wsStoreResidentCount(); got != 0 {
		t.Fatalf("expected all expired entries reclaimed, %d remain", got)
	}
	if got := wsConversationStoreStats.entries.Load(); got != 0 {
		t.Fatalf("expected entry counter back to 0, got %d", got)
	}
	if got := wsConversationStoreStats.totalSize.Load(); got != 0 {
		t.Fatalf("expected size counter back to 0, got %d", got)
	}
}

// Unexpired entries must survive a sweep.
func TestSweepKeepsLiveWSConversationStates(t *testing.T) {
	resetWSConversationStateStore()
	t.Cleanup(resetWSConversationStateStore)

	storeWSConversationState(1, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_live",
		ChannelID:           2,
	}, time.Hour)
	storeWSConversationState(1, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_dead",
		ChannelID:           3,
	}, time.Millisecond)

	time.Sleep(10 * time.Millisecond)
	sweepExpiredWSConversationStates()

	if state := loadWSConversationState(1, "gpt-4o", "ws_live"); state == nil {
		t.Fatal("expected the unexpired entry to survive the sweep")
	}
	if state := loadWSConversationState(1, "gpt-4o", "ws_dead"); state != nil {
		t.Fatal("expected the expired entry to be gone")
	}
}

// Closing a connection releases every model's state for that session, without
// touching other connections.
func TestDeleteWSConversationStatesBySession(t *testing.T) {
	resetWSConversationStateStore()
	t.Cleanup(resetWSConversationStateStore)

	for _, requestModel := range []string{"gpt-4o", "gpt-4o-mini"} {
		storeWSConversationState(5, requestModel, &wsConversationState{
			DownstreamSessionID: "ws_closing",
			ChannelID:           1,
		}, time.Hour)
	}
	storeWSConversationState(5, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_other",
		ChannelID:           1,
	}, time.Hour)
	storeWSConversationState(6, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_closing",
		ChannelID:           1,
	}, time.Hour)

	deleteWSConversationStatesBySession(5, "ws_closing")

	if state := loadWSConversationState(5, "gpt-4o", "ws_closing"); state != nil {
		t.Fatal("expected the closed session's state to be released")
	}
	if state := loadWSConversationState(5, "gpt-4o-mini", "ws_closing"); state != nil {
		t.Fatal("expected every model of the closed session to be released")
	}
	if state := loadWSConversationState(5, "gpt-4o", "ws_other"); state == nil {
		t.Fatal("expected another live session on the same key to survive")
	}
	if state := loadWSConversationState(6, "gpt-4o", "ws_closing"); state == nil {
		t.Fatal("expected a same-named session under a different API key to survive")
	}
	if got := wsConversationStoreStats.entries.Load(); got != 2 {
		t.Fatalf("expected 2 entries remaining, counter says %d", got)
	}
}

// Beyond the entry cap new sessions are dropped rather than accumulated.
func TestWSConversationStoreEnforcesEntryCap(t *testing.T) {
	resetWSConversationStateStore()
	// This test pre-loads the shared counter, so make sure it cannot leak into
	// whatever runs next.
	t.Cleanup(resetWSConversationStateStore)
	wsConversationStoreStats.entries.Store(wsConversationStoreMaxEntries)

	storeWSConversationState(9, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_overflow",
		ChannelID:           1,
	}, time.Hour)

	if state := loadWSConversationState(9, "gpt-4o", "ws_overflow"); state != nil {
		t.Fatal("expected the store to reject a save past the entry cap")
	}
	if got := wsConversationStoreStats.entries.Load(); got != wsConversationStoreMaxEntries {
		t.Fatalf("expected the counter to be rolled back to the cap, got %d", got)
	}
}

// Re-storing the same key updates in place: one slot, size adjusted by delta.
func TestWSConversationStoreReplacementKeepsCountersConsistent(t *testing.T) {
	resetWSConversationStateStore()
	t.Cleanup(resetWSConversationStateStore)

	storeWSConversationState(3, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_1",
		ChannelID:           1,
		LastResponseID:      "resp_short",
	}, time.Hour)
	firstSize := wsConversationStoreStats.totalSize.Load()

	storeWSConversationState(3, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_1",
		ChannelID:           1,
		LastResponseID:      "resp_considerably_longer_identifier",
	}, time.Hour)

	if got := wsConversationStoreStats.entries.Load(); got != 1 {
		t.Fatalf("expected the replacement to reuse one slot, counter says %d", got)
	}
	if got := wsConversationStoreStats.totalSize.Load(); got <= firstSize {
		t.Fatalf("expected size to grow with the longer payload (%d -> %d)", firstSize, got)
	}
	if got := wsStoreResidentCount(); got != 1 {
		t.Fatalf("expected exactly 1 resident entry, got %d", got)
	}
}

// Explicit deletion must also decrement the counters.
func TestDeleteWSConversationStateUpdatesCounters(t *testing.T) {
	resetWSConversationStateStore()
	t.Cleanup(resetWSConversationStateStore)

	storeWSConversationState(4, "gpt-4o", &wsConversationState{
		DownstreamSessionID: "ws_x",
		ChannelID:           1,
	}, time.Hour)
	deleteWSConversationState(4, "gpt-4o", "ws_x")

	if got := wsConversationStoreStats.entries.Load(); got != 0 {
		t.Fatalf("expected entry counter 0 after delete, got %d", got)
	}
	if got := wsConversationStoreStats.totalSize.Load(); got != 0 {
		t.Fatalf("expected size counter 0 after delete, got %d", got)
	}
}
