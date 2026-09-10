package model

import "context"

// SourceEvent is the transport-neutral provider event passed to outbound
// transformers. Envelope metadata remains separate from Data so an adapter can
// trust the transport event type even when the payload is empty, non-JSON, or
// carries a conflicting provider field.
type SourceEvent struct {
	Type      string
	Data      []byte
	ID        string
	Sequence  int64
	Transport string
	// Decoded is an immutable provider DTO cached by an inspector. Changing
	// Data requires clearing it. Transport metadata stays authoritative.
	Decoded any `json:"-"`
}

type StreamEventPreview struct {
	EventType string
	EventID   string
	Semantic  bool
	Terminal  bool
}

// SourceEventInspector parses without advancing provider stream state. A raw
// transport may preview a complete data line, then reuse the DTO after the
// remaining SSE metadata and delimiter arrive.
type SourceEventInspector interface {
	InspectSourceEvent(context.Context, SourceEvent) (SourceEvent, StreamEventPreview, error)
}

type SourceEventInspectorFunc func(context.Context, SourceEvent) (SourceEvent, StreamEventPreview, error)

func (f SourceEventInspectorFunc) InspectSourceEvent(ctx context.Context, event SourceEvent) (SourceEvent, StreamEventPreview, error) {
	return f(ctx, event)
}

const (
	SourceTransportSSE       = "sse"
	SourceTransportWebSocket = "websocket"
	SourceTransportRaw       = "raw"
)
