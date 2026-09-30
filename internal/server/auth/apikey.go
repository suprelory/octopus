package auth

import (
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// ValidateAPIKey is shared by HTTP requests and individual WebSocket turns.
// The upgrade only checks permissions; each response.create consumes its RPM.
func ValidateAPIKey(key model.APIKey, checkRPM bool) (*apperror.Error, int) {
	if !key.Enabled {
		return apperror.New(apperror.CodeAuthAPIKeyDisabled, "API key is disabled").WithStatus(http.StatusUnauthorized), 0
	}
	if key.ExpireAt > 0 && key.ExpireAt <= time.Now().Unix() {
		return apperror.New(apperror.CodeAuthAPIKeyExpired, "API key has expired").WithStatus(http.StatusUnauthorized), 0
	}
	stats := op.StatsAPIKeyGet(key.ID)
	if key.MaxCost > 0 && key.MaxCost <= stats.OutputCost+stats.InputCost {
		return apperror.New(apperror.CodeAuthAPIKeyCostExceeded, "API key has reached the max cost").WithStatus(http.StatusUnauthorized), 0
	}
	if checkRPM && key.MaxRPM > 0 {
		if allowed, retryAfter := op.RateLimitCheck(key.ID, key.MaxRPM); !allowed {
			return apperror.New(apperror.CodeAuthAPIKeyRateLimited, "API key has exceeded the rate limit").WithStatus(http.StatusTooManyRequests), retryAfter
		}
	}
	return nil, 0
}
