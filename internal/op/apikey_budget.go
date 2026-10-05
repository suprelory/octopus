package op

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

var (
	ErrAPIKeyUnavailable  = errors.New("API key is unavailable")
	ErrAPIKeyCostExceeded = errors.New("API key has reached the max cost")
	ErrAPIKeyCostInFlight = errors.New("API key budget is reserved by an in-flight request")
)

var apiKeyCostLock sync.Mutex
var apiKeyInFlight = make(map[int]int)

// APIKeyCostReservation reserves the entire remaining balance for a limited
// key. This deliberately permits one billable request at a time: estimates are
// not accurate enough to safely divide a small balance among concurrent calls.
// Unlimited requests are counted too, so lowering a limit waits for them.
type APIKeyCostReservation struct {
	keyID     int
	limited   bool
	remaining float64
	attempted float64
	released  bool
}

type apiKeyCostContextKey struct{}

func WithAPIKeyCostReservation(ctx context.Context, reservation *APIKeyCostReservation) context.Context {
	return context.WithValue(ctx, apiKeyCostContextKey{}, reservation)
}

func APIKeyCostReservationFromContext(ctx context.Context) *APIKeyCostReservation {
	if ctx == nil {
		return nil
	}
	reservation, _ := ctx.Value(apiKeyCostContextKey{}).(*APIKeyCostReservation)
	return reservation
}

func (r *APIKeyCostReservation) Limited() bool { return r != nil && r.limited }

func APIKeyReserveCost(id int) (*APIKeyCostReservation, error) {
	apiKeyWriteLock.RLock()
	defer apiKeyWriteLock.RUnlock()
	statsLifecycleLock.RLock()
	defer statsLifecycleLock.RUnlock()
	apiKeyCostLock.Lock()
	defer apiKeyCostLock.Unlock()
	key, exists := apiKeyCache.Get(id)
	if !exists || !key.Enabled || (key.ExpireAt > 0 && key.ExpireAt <= time.Now().Unix()) {
		return nil, ErrAPIKeyUnavailable
	}
	if key.MaxCost < 0 || math.IsNaN(key.MaxCost) || math.IsInf(key.MaxCost, 0) {
		return nil, ErrAPIKeyCostExceeded
	}
	r := &APIKeyCostReservation{keyID: id, limited: key.MaxCost > 0}
	if r.limited {
		stats := statsAPIKeyGet(id)
		r.remaining = key.MaxCost - stats.InputCost - stats.OutputCost
		if r.remaining <= 0 || math.IsNaN(r.remaining) || math.IsInf(r.remaining, 0) {
			return nil, ErrAPIKeyCostExceeded
		}
		if apiKeyInFlight[id] > 0 {
			return nil, ErrAPIKeyCostInFlight
		}
	}
	apiKeyInFlight[id]++
	return r, nil
}

// ReserveAttempt bounds the sum of estimated maximum costs across retries and
// transport recovery. A send failure still consumes its allowance because the
// upstream may have accepted it. No shared lock is held during network I/O.
func (r *APIKeyCostReservation) ReserveAttempt(cost float64) error {
	if r == nil || !r.limited {
		return nil
	}
	apiKeyWriteLock.RLock()
	defer apiKeyWriteLock.RUnlock()
	statsLifecycleLock.RLock()
	defer statsLifecycleLock.RUnlock()
	apiKeyCostLock.Lock()
	defer apiKeyCostLock.Unlock()
	key, exists := apiKeyCache.Get(r.keyID)
	if r.released || !exists || !key.Enabled || (key.ExpireAt > 0 && key.ExpireAt <= time.Now().Unix()) {
		return ErrAPIKeyUnavailable
	}
	remaining := r.remaining
	if key.MaxCost > 0 {
		stats := statsAPIKeyGet(r.keyID)
		remaining = math.Min(remaining, key.MaxCost-stats.InputCost-stats.OutputCost)
	}
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) || math.IsNaN(remaining) || r.attempted+cost > remaining {
		return ErrAPIKeyCostExceeded
	}
	r.attempted += cost
	return nil
}

// Release must run after final statistics settlement, including error/cancel
// paths. Keep reservations across config refresh, key deletion and restore;
// only the owning request can release one, and repeated cleanup is harmless.
func (r *APIKeyCostReservation) Release() {
	if r == nil {
		return
	}
	apiKeyCostLock.Lock()
	defer apiKeyCostLock.Unlock()
	if r.released {
		return
	}
	r.released = true
	apiKeyInFlight[r.keyID]--
	if apiKeyInFlight[r.keyID] == 0 {
		delete(apiKeyInFlight, r.keyID)
	}
}
