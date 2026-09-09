package stream

import (
	"context"
	"io"
)

// RawSource reads raw bytes in fixed-size chunks (for passthrough).
type RawSource struct {
	reader   io.ReadCloser
	bufSize  int
	sequence int64
}

// NewRawSource creates a source that reads raw chunks.
func NewRawSource(reader io.ReadCloser, bufSize int) *RawSource {
	if bufSize <= 0 {
		bufSize = 32 * 1024 // 32KB default
	}
	return &RawSource{
		reader:  reader,
		bufSize: bufSize,
	}
}

// ReadEvent reads the next chunk of raw bytes.
func (s *RawSource) ReadEvent(ctx context.Context) ([]byte, error) {
	event, err := s.ReadSourceEvent(ctx)
	if err != nil {
		return nil, err
	}
	return event.Data, nil
}

// ReadSourceEvent returns raw chunks with a stable source identity and sequence
// number. Raw chunks intentionally have no event type.
func (s *RawSource) ReadSourceEvent(ctx context.Context) (SourceEvent, error) {
	buf := make([]byte, s.bufSize)
	n, err := s.reader.Read(buf)
	if n > 0 {
		// Return a copy to avoid buffer reuse issues
		chunk := make([]byte, n)
		copy(chunk, buf[:n])
		s.sequence++
		return SourceEvent{Data: chunk, Sequence: s.sequence, Transport: SourceTransportRaw}, nil
	}
	if err != nil {
		return SourceEvent{}, err
	}
	return SourceEvent{}, nil
}

// Close releases the underlying reader.
func (s *RawSource) Close() error {
	if s.reader != nil {
		return s.reader.Close()
	}
	return nil
}
