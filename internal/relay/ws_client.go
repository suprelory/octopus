package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const (
	wsClientMaxAge    = 60 * time.Minute
	wsClientReadLimit = 16 * 1024 * 1024 // 16MB per message
)

// HandleWSResponse handles WebSocket upgrade for /v1/responses.
func HandleWSResponse(c *gin.Context) {
	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow cross-origin
	})
	if err != nil {
		log.Warnf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.CloseNow()

	conn.SetReadLimit(wsClientReadLimit)

	ctx, cancel := context.WithTimeout(c.Request.Context(), wsClientMaxAge)
	defer cancel()

	apiKeyID := c.GetInt("api_key_id")
	supportedModels := c.GetString("supported_models")
	clientIP := middleware.ClientIP(c)

	log.Debugf("ws client connected (apikey=%d)", apiKeyID)

	downstreamSessionID := fmt.Sprintf("ws_%d", time.Now().UnixNano())
	// The session ID is unique to this connection, so any state left behind is
	// unreachable once we return. Release it here instead of waiting for the
	// sweeper to notice the TTL.
	defer deleteWSConversationStatesBySession(apiKeyID, downstreamSessionID)
	var conversationState *wsConversationState

	// Message loop
	for {
		select {
		case <-ctx.Done():
			writeWSError(ctx, conn, 400, "websocket_connection_limit_reached",
				"Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.")
			conn.Close(websocket.StatusNormalClosure, "connection limit reached")
			return
		default:
		}

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
				log.Debugf("ws client disconnected normally (apikey=%d)", apiKeyID)
			} else {
				log.Warnf("ws client read error (apikey=%d): %v", apiKeyID, err)
			}
			return
		}

		if msgType != websocket.MessageText {
			continue
		}

		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse message")
			continue
		}

		if msg.Type != "response.create" {
			writeWSError(ctx, conn, 400, "invalid_request",
				fmt.Sprintf("Unknown message type: %s", msg.Type))
			continue
		}

		conversationState = processWSResponseCreate(ctx, conn, data, apiKeyID, supportedModels, clientIP, downstreamSessionID, conversationState)
	}
}

func writeWSError(ctx context.Context, conn *websocket.Conn, status int, code, message string, retryAt ...time.Time) {
	deadline := time.Time{}
	if len(retryAt) > 0 {
		deadline = retryAt[0]
	}
	errEvent := buildWSErrorEvent(status, code, message, deadline, time.Now())
	if err := writeWSEvent(ctx, conn, errEvent); err != nil {
		log.Debugf("ws error event write failed: %v", err)
	}
}

func buildWSErrorEvent(status int, code, message string, retryAt, now time.Time) map[string]interface{} {
	errEvent := map[string]interface{}{
		"type":   "error",
		"status": status,
		"error": map[string]interface{}{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	}
	if value := retryAfterHeaderValue(retryAt, now); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			errEvent["retry_after"] = seconds
		}
	}
	return errEvent
}
