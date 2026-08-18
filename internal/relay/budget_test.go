package relay

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRelayFailoverBudgetEnforcesChannelAndAttemptLimits(t *testing.T) {
	now := time.Now()
	budget := &relayFailoverBudget{
		maxChannelAttempts: 2,
		maxTotalAttempts:   3,
		deadline:           now.Add(time.Minute),
	}

	if err := budget.reserveChannel(11, now); err != nil {
		t.Fatalf("reserve first channel: %v", err)
	}
	if err := budget.reserveChannel(22, now); err != nil {
		t.Fatalf("reserve second channel: %v", err)
	}
	if err := budget.reserveChannel(22, now); err != nil {
		t.Fatalf("reserve duplicate channel: %v", err)
	}
	if !budget.canAttemptChannel(22, now) {
		t.Fatal("expected a previously visited channel to remain within the channel budget")
	}
	if budget.canAttemptChannel(33, now) {
		t.Fatal("did not expect a new channel after exhausting the distinct-channel budget")
	}
	if err := budget.reserveChannel(33, now); !errors.Is(err, errLocalRelayBudgetExceeded) {
		t.Fatalf("third channel error = %v, want budget exceeded", err)
	}

	for attempt := 0; attempt < 3; attempt++ {
		if err := budget.reserveAttempt(now); err != nil {
			t.Fatalf("reserve attempt %d: %v", attempt+1, err)
		}
	}
	if err := budget.reserveAttempt(now); !errors.Is(err, errLocalRelayBudgetExceeded) {
		t.Fatalf("fourth attempt error = %v, want budget exceeded", err)
	}
}

func TestRelayBudgetFailureUsesTimeoutProtocolWithoutBreakerRecord(t *testing.T) {
	err := newRelayBudgetError("test limit")
	classification := classifyRelayFailure(0, err, time.Time{})
	if classification.Class != FailureBudgetExceeded || classification.Retryable || classification.Record {
		t.Fatalf("budget classification = %#v", classification)
	}

	responseErr := protocolErrorForAttempt(relayBudgetAttemptResult(err), err)
	if responseErr == nil || responseErr.StatusCode != http.StatusGatewayTimeout || responseErr.Detail.Code != CodeRelayTimeout {
		t.Fatalf("budget protocol error = %#v", responseErr)
	}
	statusBearingResult := relayBudgetAttemptResult(err)
	statusBearingResult.StatusCode = http.StatusTooManyRequests
	if responseErr = protocolErrorForAttempt(statusBearingResult, err); responseErr == nil || responseErr.StatusCode != http.StatusGatewayTimeout || responseErr.Detail.Code != CodeRelayTimeout {
		t.Fatalf("status-bearing budget protocol error = %#v", responseErr)
	}
}

func TestSameChannelMaxAttemptsIncludesInitialRequest(t *testing.T) {
	if got := sameChannelMaxAttempts(false, 9); got != 1 {
		t.Fatalf("disabled retry attempts = %d, want 1", got)
	}
	if got := sameChannelMaxAttempts(true, 3); got != 3 {
		t.Fatalf("configured attempts = %d, want 3 including initial request", got)
	}
	if got := sameChannelMaxAttempts(true, 0); got != defaultSameChannelMaxAttempts {
		t.Fatalf("fallback attempts = %d, want %d", got, defaultSameChannelMaxAttempts)
	}
}
