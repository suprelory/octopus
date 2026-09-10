package model

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
}

const (
	SourceTransportSSE       = "sse"
	SourceTransportWebSocket = "websocket"
	SourceTransportRaw       = "raw"
)
