package stream

import (
	"context"
	"errors"
	"io"
	"sync"
)

// SSESource has exactly one underlying reader. Concurrent consumers receive
// producer-assigned sequences; Close cancels delivery and unblocks the reader.
type SSESource struct {
	reader    io.ReadCloser
	decoder   *SSEDecoder
	events    chan sseReadResult
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

type sseReadResult struct {
	event SourceEvent
	err   error
}

func NewSSESource(reader io.ReadCloser, maxEventSize int) *SSESource {
	s := &SSESource{reader: reader, decoder: NewSSEDecoder(maxEventSize), events: make(chan sseReadResult, 1), done: make(chan struct{})}
	go s.readLoop()
	return s
}

func (s *SSESource) deliver(result sseReadResult) error {
	select {
	case <-s.done:
		return io.ErrClosedPipe
	case s.events <- result:
		return nil
	}
}

func (s *SSESource) readLoop() {
	defer close(s.events)
	emit := func(event SourceEvent) error { return s.deliver(sseReadResult{event: event}) }
	buffer := make([]byte, 4*1024)
	emptyReads := 0
	for {
		n, err := s.reader.Read(buffer)
		if n > 0 {
			emptyReads = 0
			if decodeErr := s.decoder.Feed(buffer[:n], emit); decodeErr != nil {
				_ = s.deliver(sseReadResult{err: decodeErr})
				return
			}
		} else if err == nil {
			emptyReads++
			if emptyReads >= 100 {
				err = io.ErrNoProgress
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if finishErr := s.decoder.Finish(emit); finishErr != nil {
					_ = s.deliver(sseReadResult{err: finishErr})
				}
			} else {
				_ = s.deliver(sseReadResult{err: err})
			}
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}

func (s *SSESource) ReadEvent(ctx context.Context) ([]byte, error) {
	event, err := s.ReadSourceEvent(ctx)
	return event.Data, err
}

func (s *SSESource) ReadSourceEvent(ctx context.Context) (SourceEvent, error) {
	if err := ctx.Err(); err != nil {
		return SourceEvent{}, err
	}
	select {
	case <-ctx.Done():
		return SourceEvent{}, ctx.Err()
	case <-s.done:
		return SourceEvent{}, io.ErrClosedPipe
	case result, ok := <-s.events:
		if !ok {
			select {
			case <-s.done:
				return SourceEvent{}, io.ErrClosedPipe
			default:
				return SourceEvent{}, io.EOF
			}
		}
		return result.event, result.err
	}
}

func (s *SSESource) ReadEventWithType(ctx context.Context) (SourceEvent, error) {
	return s.ReadSourceEvent(ctx)
}

func (s *SSESource) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.reader != nil {
			s.closeErr = s.reader.Close()
		}
	})
	return s.closeErr
}
