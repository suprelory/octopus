package middleware

import (
	"net/http"
	"strconv"
	"strings"

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
	return apiKeyAuth(true)
}

// APIKeyWSAuth is only for the WebSocket upgrade route. Message admission
// performs rate limiting inside the connection instead of charging the upgrade.
func APIKeyWSAuth() gin.HandlerFunc {
	return apiKeyAuth(false)
}

func apiKeyAuth(checkRPM bool) gin.HandlerFunc {
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
		if err != nil || apiKeyObj.APIKey != apiKey {
			RecordAuthEvent(c, "api_key.rejected", apperror.CodeAuthInvalidToken, 0)
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		if authErr, retryAfter := auth.ValidateAPIKey(apiKeyObj, checkRPM); authErr != nil {
			RecordAuthEvent(c, "api_key.rejected", authErr.Code, apiKeyObj.ID)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			resp.ErrorWithAppError(c, authErr.Status, authErr)
			c.Abort()
			return
		}
		c.Set("authenticated_api_key", apiKey)
		c.Set("request_type", requestType)
		c.Set("supported_models", apiKeyObj.SupportedModels)
		c.Set("api_key_id", apiKeyObj.ID)
		if checkRPM && c.Request.Method == http.MethodPost {
			reservation, authErr := auth.ReserveAPIKeyCost(apiKeyObj)
			if authErr != nil {
				RecordAuthEvent(c, "api_key.rejected", authErr.Code, apiKeyObj.ID)
				if authErr.Status == http.StatusTooManyRequests {
					c.Header("Retry-After", "1")
				}
				resp.ErrorWithAppError(c, authErr.Status, authErr)
				c.Abort()
				return
			}
			defer reservation.Release()
			c.Request = c.Request.WithContext(op.WithAPIKeyCostReservation(c.Request.Context(), reservation))
		}
		c.Next()
	}
}
