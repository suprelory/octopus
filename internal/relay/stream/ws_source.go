package stream

import (
	"context"
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
	reader WSUpstreamReader
}

// NewWSSource creates a source from a WebSocket reader.
func NewWSSource(reader WSUpstreamReader) *WSSource {
	return &WSSource{reader: reader}
}

// ReadEvent reads the next WebSocket event.
func (s *WSSource) ReadEvent(ctx context.Context) ([]byte, error) {
	return s.reader.ReadEvent(ctx)
}

// The relay owns the connection lease: it returns a connection to the pool only
// after stream validation succeeds. The processor cancels and joins its reader.
func (s *WSSource) Close() error {
	return nil
}
