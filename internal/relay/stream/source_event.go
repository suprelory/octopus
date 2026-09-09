package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// SourceEvent is the transport-neutral envelope passed between stream sources
// and transformers. Type and ID stay separate from Data because compatible
// providers sometimes send terminal metadata outside the payload or leave the
// data field empty.
type SourceEvent struct {
	Type      string
	Data      []byte
	ID        string
	Sequence  int64
	Transport string
}

const (
	SourceTransportSSE       = "sse"
	SourceTransportWebSocket = "websocket"
	SourceTransportRaw       = "raw"
)

// SourceEventSource is the preferred stream source contract. It keeps transport
// metadata alongside the payload so protocol adapters do not need to infer an
// event type from JSON or lose the SSE envelope while crossing the relay.
type SourceEventSource interface {
	ReadSourceEvent(ctx context.Context) (SourceEvent, error)
}

// TypedStreamSource is the legacy type-aware source contract. New sources
// should implement SourceEventSource; StreamProcessor still accepts this
// interface so custom sources can migrate without an atomic API break.
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
