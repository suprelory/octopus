package relay

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	if len(retryAt) > 0 && string(retryAt) != "null" {
		var value string
		if json.Unmarshal(retryAt, &value) == nil {
			value = strings.TrimSpace(value)
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed
			}
			if parsed, err := time.Parse(time.RFC3339, value); err == nil {
				return parsed
			}
			if parsed := parseRetryAtAt(value, now); !parsed.IsZero() {
				return parsed
			}
		}
	}
	if len(retryAfter) == 0 || string(retryAfter) == "null" {
		return time.Time{}
	}
	var value string
	if json.Unmarshal(retryAfter, &value) == nil {
		return parseRetryAtAt(value, now)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(retryAfter)), 64)
	if err != nil || seconds < 0 || seconds > float64((time.Duration(1<<63-1))/time.Second) {
		return time.Time{}
	}
	return now.Add(time.Duration(seconds * float64(time.Second)))
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
	switch strings.TrimSpace(eventType) {
	case "response.completed",
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.canceled",
		"response.error",
		"response.done":
		return true
	default:
		return false
	}
}

func isWSStreamErrorEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "error", "response.failed", "response.error":
		return true
	default:
		return false
	}
}
