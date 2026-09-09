package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// SourceEvent carries the optional SSE event type together with its payload.
// The type is kept separate because compatible providers sometimes send a
// terminal event in the SSE envelope while leaving data empty or untyped.
type SourceEvent struct {
	Type string
	Data []byte
}

// TypedStreamSource is an optional extension to StreamSource. Existing raw and
// WebSocket sources can keep implementing ReadEvent; SSESource implements this
// interface when the envelope event type is needed by a transformer.
type TypedStreamSource interface {
	ReadEventWithType(ctx context.Context) (SourceEvent, error)
}

// NormalizeEventData makes an SSE envelope type visible to JSON transformers.
// It only changes empty or object payloads that do not already contain a type.
// Non-JSON payloads and payloads with an existing type are returned unchanged.
func NormalizeEventData(eventType string, data []byte) []byte {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return data
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		payload, err := json.Marshal(struct {
			Type string `json:"type"`
		}{Type: eventType})
		if err == nil {
			return payload
		}
		return data
	}

	if trimmed[0] != '{' {
		return data
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return data
	}
	if rawType, exists := object["type"]; exists {
		var existingType string
		if json.Unmarshal(rawType, &existingType) == nil && strings.TrimSpace(existingType) != "" {
			return data
		}
	}
	typeValue, err := json.Marshal(eventType)
	if err != nil {
		return data
	}
	object["type"] = typeValue
	normalized, err := json.Marshal(object)
	if err != nil {
		return data
	}
	return normalized
}
