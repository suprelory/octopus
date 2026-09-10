package stream

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// WSUpstreamReader abstracts WebSocket upstream reader interface.
// This avoids circular dependency with internal/relay package.
type WSUpstreamReader interface {
	ReadEvent(ctx context.Context) ([]byte, error)
	Close() error
	CloseWithError()
	StatusCode() int
}

// WSSource wraps a WebSocket upstream reader.
type WSSource struct {
	reader   WSUpstreamReader
	sequence int64
	readMu   sync.Mutex
}

// NewWSSource creates a source from a WebSocket reader.
func NewWSSource(reader WSUpstreamReader) *WSSource {
	return &WSSource{reader: reader}
}

// ReadEvent reads the next WebSocket event.
func (s *WSSource) ReadEvent(ctx context.Context) ([]byte, error) {
	event, err := s.ReadSourceEvent(ctx)
	if err != nil {
		return nil, err
	}
	return event.Data, nil
}

// ReadSourceEvent normalizes WebSocket messages into the same envelope used by
// SSE and raw sources. WebSocket has no separate event field, so type and ID
// are read from the JSON envelope when present.
func (s *WSSource) ReadSourceEvent(ctx context.Context) (SourceEvent, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	data, err := s.reader.ReadEvent(ctx)
	if err != nil {
		return SourceEvent{}, err
	}
	s.sequence++
	typ, id := websocketEventMetadata(data)
	return SourceEvent{
		Type:      typ,
		Data:      data,
		ID:        id,
		Sequence:  s.sequence,
		Transport: SourceTransportWebSocket,
	}, nil
}

func websocketEventMetadata(data []byte) (typ, id string) {
	var envelope struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		EventID     string `json:"event_id"`
		LastEventID string `json:"last_event_id"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return "", ""
	}
	typ = strings.TrimSpace(envelope.Type)
	id = strings.TrimSpace(envelope.ID)
	if id == "" {
		id = strings.TrimSpace(envelope.EventID)
	}
	if id == "" {
		id = strings.TrimSpace(envelope.LastEventID)
	}
	return typ, id
}

// The relay owns the connection lease and decides whether it can be reused.
// The processor cancels and joins its reader before returning.
func (s *WSSource) Close() error {
	return nil
}
