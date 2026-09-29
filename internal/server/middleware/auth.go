package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			RecordAuthEvent(c, "admin.rejected", apperror.CodeAuthUnauthorized, 0)
			resp.Unauthorized(c)
			c.Abort()
			return
		}
		if !auth.VerifyJWTToken(strings.TrimPrefix(token, "Bearer ")) {
			RecordAuthEvent(c, "admin.rejected", apperror.CodeAuthInvalidToken, 0)
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		c.Set(adminAuthenticatedKey, true)
		c.Next()
	}
}

func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var apiKey string
		var requestType string

		if key := c.Request.Header.Get("x-api-key"); key != "" {
			apiKey = key
			requestType = "anthropic"
		} else if auth := c.Request.Header.Get("Authorization"); auth != "" {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
			requestType = "openai"
		}

		if apiKey == "" {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthAPIKeyMissing, 0)
			resp.APIKeyMissing(c)
			c.Abort()
			return
		}

		if !strings.HasPrefix(apiKey, "sk-"+conf.APP_NAME+"-") {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthInvalidToken, 0)
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		apiKeyObj, err := op.APIKeyGetByAPIKey(apiKey, c.Request.Context())
		if err != nil {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthInvalidToken, 0)
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		if !apiKeyObj.Enabled {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthAPIKeyDisabled, apiKeyObj.ID)
			resp.ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyDisabled, "API key is disabled").WithStatus(http.StatusUnauthorized))
			c.Abort()
			return
		}
		if apiKeyObj.ExpireAt > 0 && apiKeyObj.ExpireAt < time.Now().Unix() {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthAPIKeyExpired, apiKeyObj.ID)
			resp.APIKeyExpired(c)
			c.Abort()
			return
		}
		statsAPIKey := op.StatsAPIKeyGet(apiKeyObj.ID)
		if apiKeyObj.MaxCost > 0 && apiKeyObj.MaxCost < statsAPIKey.StatsMetrics.OutputCost+statsAPIKey.StatsMetrics.InputCost {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthAPIKeyCostExceeded, apiKeyObj.ID)
			resp.ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyCostExceeded, "API key has reached the max cost").WithStatus(http.StatusUnauthorized))
			c.Abort()
			return
		}
		if apiKeyObj.MaxRPM > 0 {
			allowed, retryAfter := op.RateLimitCheck(apiKeyObj.ID, apiKeyObj.MaxRPM)
			if !allowed {
				RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthAPIKeyRateLimited, apiKeyObj.ID)
				c.Header("Retry-After", strconv.Itoa(retryAfter))
				resp.ErrorWithAppError(c, http.StatusTooManyRequests, apperror.New(apperror.CodeAuthAPIKeyRateLimited, "API key has exceeded the rate limit").WithStatus(http.StatusTooManyRequests))
				c.Abort()
				return
			}
		}
		c.Set("request_type", requestType)
		c.Set("supported_models", apiKeyObj.SupportedModels)
		c.Set("api_key_id", apiKeyObj.ID)
		c.Next()
	}
}
