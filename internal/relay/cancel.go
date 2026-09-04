package relay

import (
	"context"
	"errors"
)

var (
	errLocalRelayBudgetExceeded = errors.New("local relay budget exceeded")
	errFirstTokenTimeout        = errors.New("first token timeout")
)

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func isLocalRelayBudgetError(err error) bool {
	return errors.Is(err, errLocalRelayBudgetExceeded)
}

func isLocalRelayBudgetExceeded(ctx context.Context, err error) bool {
	if isLocalRelayBudgetError(err) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errLocalRelayBudgetExceeded)
}

func isFirstTokenTimeoutError(err error) bool {
	return errors.Is(err, errFirstTokenTimeout)
}

func isFirstTokenTimeout(ctx context.Context, err error) bool {
	if isFirstTokenTimeoutError(err) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errFirstTokenTimeout)
}

func isClientCancellation(ctx context.Context, err error) bool {
	if isLocalRelayBudgetExceeded(ctx, err) || isFirstTokenTimeout(ctx, err) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}
