package op

import (
	"testing"
	"time"
)

func loginGuardReset() {
	loginGuardLock.Lock()
	loginGuard = make(map[string]*loginGuardEntry)
	loginGuardLastPrune = time.Time{}
	loginGuardLock.Unlock()
}

func TestLoginAttemptAllowRPM(t *testing.T) {
	loginGuardReset()
	t.Cleanup(loginGuardReset)

	const source = "203.0.113.10"

	for i := 0; i < loginMaxRPM; i++ {
		allowed, retryAfter := LoginAttemptAllow(source)
		if !allowed {
			t.Fatalf("attempt %d was rejected before the RPM cap (retryAfter=%d)", i+1, retryAfter)
		}
	}

	allowed, retryAfter := LoginAttemptAllow(source)
	if allowed {
		t.Fatal("expected the attempt past the RPM cap to be rejected")
	}
	if retryAfter < 1 {
		t.Fatalf("retryAfter = %d, want >= 1", retryAfter)
	}
}

func TestLoginAttemptBackoffAfterFailures(t *testing.T) {
	loginGuardReset()
	t.Cleanup(loginGuardReset)

	const source = "203.0.113.11"

	// 阈值以内的失败不应该触发锁定。
	for i := 0; i < loginFailureThreshold; i++ {
		LoginAttemptFailed(source)
	}
	if allowed, _ := LoginAttemptAllow(source); !allowed {
		t.Fatal("expected attempts within the failure threshold to stay allowed")
	}

	// 超过阈值后进入退避。
	LoginAttemptFailed(source)
	allowed, retryAfter := LoginAttemptAllow(source)
	if allowed {
		t.Fatal("expected the source to be locked out after exceeding the failure threshold")
	}
	if retryAfter < 1 {
		t.Fatalf("retryAfter = %d, want >= 1", retryAfter)
	}
}

func TestLoginAttemptSucceededClearsBackoff(t *testing.T) {
	loginGuardReset()
	t.Cleanup(loginGuardReset)

	const source = "203.0.113.12"

	for i := 0; i < loginFailureThreshold+1; i++ {
		LoginAttemptFailed(source)
	}
	if allowed, _ := LoginAttemptAllow(source); allowed {
		t.Fatal("expected the source to be locked out")
	}

	LoginAttemptSucceeded(source)
	if allowed, _ := LoginAttemptAllow(source); !allowed {
		t.Fatal("expected a successful login to clear the backoff")
	}
}

func TestLoginBackoffGrowsAndCaps(t *testing.T) {
	first := loginBackoff(loginFailureThreshold + 1)
	second := loginBackoff(loginFailureThreshold + 2)
	if second <= first {
		t.Fatalf("backoff did not grow: first=%v second=%v", first, second)
	}
	if capped := loginBackoff(loginFailureThreshold + 1000); capped != loginBackoffMax {
		t.Fatalf("backoff for a large failure count = %v, want the %v cap", capped, loginBackoffMax)
	}
	// 移位溢出会让 Duration 变成负数，必须落到上限而不是变成「立刻放行」。
	if capped := loginBackoff(1 << 30); capped != loginBackoffMax {
		t.Fatalf("backoff overflowed to %v, want the %v cap", capped, loginBackoffMax)
	}
}

func TestLoginGuardIsolatesSources(t *testing.T) {
	loginGuardReset()
	t.Cleanup(loginGuardReset)

	const locked = "203.0.113.13"
	const other = "203.0.113.14"

	for i := 0; i < loginFailureThreshold+1; i++ {
		LoginAttemptFailed(locked)
	}
	if allowed, _ := LoginAttemptAllow(locked); allowed {
		t.Fatal("expected the failing source to be locked out")
	}
	if allowed, _ := LoginAttemptAllow(other); !allowed {
		t.Fatal("one source's lockout must not affect another")
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want int
	}{
		{0, 1},
		{time.Millisecond, 1},
		{time.Second, 1},
		{1500 * time.Millisecond, 2},
		{2 * time.Second, 2},
		{-time.Second, 1},
	}
	for _, tc := range cases {
		if got := retryAfterSeconds(tc.in); got != tc.want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
