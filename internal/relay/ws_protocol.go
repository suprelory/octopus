package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

func validateWSResponseCreatePayload(payload []byte) error {
	var envelope struct {
		Type   string `json:"type"`
		Stream *bool  `json:"stream"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("invalid websocket response.create payload: %w", err)
	}
	if envelope.Type != "response.create" {
		return fmt.Errorf("websocket payload type must be response.create")
	}
	if envelope.Stream == nil || !*envelope.Stream {
		return fmt.Errorf("websocket response.create payload must use stream=true")
	}
	if strings.TrimSpace(envelope.Model) == "" {
		return fmt.Errorf("websocket response.create payload requires model")
	}
	return nil
}

func parseWSRetryDeadline(now time.Time, retryAfter, retryAt json.RawMessage) time.Time {
	return openaiOutbound.ParseStreamRetryDeadline(now, retryAfter, retryAt)
}

func firstRetryDeadline(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func isWSStreamTerminalEvent(eventType string) bool {
	return openaiOutbound.IsResponseTerminalEvent(eventType)
}

func isWSStreamErrorEvent(eventType string) bool {
	return openaiOutbound.IsResponseErrorEvent(eventType)
}
