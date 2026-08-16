package balancer

import (
	"testing"
	"time"
)

func TestResetCircuitBreakerByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	globalBreaker.Store(circuitKey(1, 10, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	globalBreaker.Store(circuitKey(10, 10, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	globalBreaker.Store(circuitKey(2, 20, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})

	ResetStateByChannel(1)

	if tripped, _ := IsTripped(1, 10, "gpt-4o"); tripped {
		t.Fatal("expected target channel circuit breaker to be reset")
	}
	if tripped, _ := IsTripped(10, 10, "gpt-4o"); !tripped {
		t.Fatal("expected channel with similar prefix to remain tripped")
	}
	if tripped, _ := IsTripped(2, 20, "gpt-4o"); !tripped {
		t.Fatal("expected unrelated channel circuit breaker to remain tripped")
	}
}

func TestResetStickyByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 100)
	SetSticky(2, "gpt-4o", 20, 200)
	SetSticky(3, "claude", 10, 300)

	ResetStateByChannel(10)

	if entry := GetSticky(1, "gpt-4o", time.Minute); entry != nil {
		t.Fatalf("expected target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(3, "claude", time.Minute); entry != nil {
		t.Fatalf("expected second target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(2, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 20 {
		t.Fatalf("expected unrelated sticky session to remain, got %#v", entry)
	}
}

func TestHalfOpenDoesNotRemainTrippedForeverWithoutResult(t *testing.T) {
	Reset()
	key := circuitKey(7, 8, "gpt-4o")
	globalBreaker.Store(key, &circuitEntry{
		State:         StateHalfOpen,
		TripCount:     1,
		HalfOpenSince: time.Now().Add(-61 * time.Second),
	})

	tripped, remaining := IsTripped(7, 8, "gpt-4o")
	if !tripped {
		t.Fatal("expected expired half-open probe to be tripped again")
	}
	if remaining <= 0 {
		t.Fatalf("expected expired half-open probe to return cooldown, got %v", remaining)
	}

	value, ok := globalBreaker.Load(key)
	if !ok {
		t.Fatal("expected circuit entry to remain after half-open timeout")
	}
	entry := value.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.State != StateOpen {
		t.Fatalf("expected expired half-open entry to return to open, got %v", entry.State)
	}
	if !entry.HalfOpenSince.IsZero() {
		t.Fatalf("expected half-open timestamp to be cleared, got %v", entry.HalfOpenSince)
	}
}

func TestRateLimitFailureSetsAbsoluteRetryAtWhileClosed(t *testing.T) {
	Reset()
	key := circuitKey(20, 30, "model")
	deadline := time.Now().Add(1500 * time.Millisecond)
	RecordFailureAt(20, 30, "model", FailureRateLimit, deadline)

	tripped, remaining := IsTripped(20, 30, "model")
	if !tripped || remaining <= time.Second {
		t.Fatalf("rate limit circuit = tripped %t remaining %v", tripped, remaining)
	}
	got, ok := RetryAt(20, 30, "model")
	if !ok || got.Before(deadline.Add(-10*time.Millisecond)) || got.After(deadline.Add(10*time.Millisecond)) {
		t.Fatalf("retry-at = %v (ok=%t), want near %v", got, ok, deadline)
	}
	if _, ok := globalBreaker.Load(key); !ok {
		t.Fatal("expected rate-limit failure to create a breaker entry")
	}
}

func TestIgnoredFailureDoesNotCreateCircuitEntry(t *testing.T) {
	Reset()
	RecordFailure(21, 31, "model", FailureIgnored)
	if _, ok := globalBreaker.Load(circuitKey(21, 31, "model")); ok {
		t.Fatal("ignored failure must not create a breaker entry")
	}
}

func TestPermanentFailureUsesLongerAbsoluteCooldown(t *testing.T) {
	Reset()
	RecordFailureAt(22, 32, "model", FailureModelUnsupported, time.Time{})
	deadline, ok := RetryAt(22, 32, "model")
	if !ok {
		t.Fatal("expected model failure to open the breaker")
	}
	remaining := time.Until(deadline)
	if remaining < 11*time.Hour || remaining > 12*time.Hour+time.Second {
		t.Fatalf("model unsupported cooldown = %v, want about 12h", remaining)
	}

	Reset()
	RecordFailureAt(23, 33, "model", FailureAuthentication, time.Time{})
	deadline, ok = RetryAt(23, 33, "model")
	if !ok {
		t.Fatal("expected authentication failure to open the breaker")
	}
	remaining = time.Until(deadline)
	if remaining < 29*time.Minute || remaining > 30*time.Minute+time.Second {
		t.Fatalf("authentication cooldown = %v, want about 30m", remaining)
	}
}

func TestOpenCircuitConcurrentFailureDoesNotShortenRetryAt(t *testing.T) {
	Reset()
	deadline := time.Now().Add(5 * time.Minute)
	RecordFailureAt(24, 34, "model", FailureRateLimit, deadline)

	// A request that was already in flight may report a later failure after the
	// circuit opened. Missing or shorter hints must not erase the exact deadline.
	RecordFailureAt(24, 34, "model", FailureTransient, time.Time{})
	RecordFailureAt(24, 34, "model", FailureRateLimit, deadline.Add(-time.Minute))

	got, ok := RetryAt(24, 34, "model")
	if !ok || !got.Equal(deadline) {
		t.Fatalf("retry-at = %v (ok=%t), want %v", got, ok, deadline)
	}
}
