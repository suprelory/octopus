package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

// ErrEmptyUpstreamStream marks 200 SSE streams that ended without forwarding
// any payload (all events skipped by transform or no events at all).
// Relay should fail over to another channel.
var ErrEmptyUpstreamStream = errors.New("upstream stream ended without forwarding any payload")

// ErrNoMeaningfulUpstreamPayload is returned when a stream only contains
// metadata/keepalive events before EOF. It wraps ErrEmptyUpstreamStream so
// existing failover checks continue to work.
var ErrNoMeaningfulUpstreamPayload = fmt.Errorf("%w: no meaningful payload", ErrEmptyUpstreamStream)

// ErrPrecommitLimitExceeded prevents metadata-only upstream streams from
// forcing an early downstream commit merely by filling the bounded buffer.
var ErrPrecommitLimitExceeded = errors.New("stream semantic precommit limit exceeded")

// StreamSource abstracts different event sources (SSE, WebSocket, raw bytes).
type StreamSource interface {
	// ReadEvent blocks until the next event is available or returns an error.
	// Returns io.EOF when the stream ends normally.
	ReadEvent(ctx context.Context) ([]byte, error)

	// Close releases resources. Must be idempotent.
	Close() error
}

// StreamTransform converts raw event data to the client's expected format.
// Returns nil/empty slice to skip writing (e.g., keep-alive events).
// For passthrough, set to nil in StreamConfig.
type StreamTransform func(ctx context.Context, data []byte) ([]byte, error)

// StreamObserver receives raw bytes without changing what is forwarded. It is
// used by passthrough streams to collect semantic events incrementally.
type StreamObserver interface {
	Observe(ctx context.Context, data []byte) error
	Finalize(ctx context.Context) error
	ReachedTerminal() bool
}

// StreamWriter abstracts the HTTP/WebSocket response writer.
type StreamWriter interface {
	Write(data []byte) (int, error)
	Flush()
	Written() bool
	Header() http.Header
	WriteHeader(code int)
}

// StreamConfig configures a StreamProcessor instance.
type StreamConfig struct {
	// Core dependencies
	Source    StreamSource
	Transform StreamTransform // nil for passthrough
	Observer  StreamObserver  // optional sidecar for passthrough observation
	Writer    StreamWriter
	Context   context.Context

	// Timeout & heartbeat
	FirstTokenTimeout time.Duration // 0 to disable
	HeartbeatInterval time.Duration // 0 to disable

	// Callbacks
	OnFirstToken func()                          // Called when first payload written
	OnFinish     func(ctx context.Context) error // Called on stream end

	// Precommit buffers transformed events until a semantic payload is seen.
	// This keeps failover possible when an upstream emits headers/metadata and
	// then fails before producing content. Limits bound memory and latency.
	PrecommitPredicate func(raw, transformed []byte) bool
	PrecommitMaxEvents int
	PrecommitMaxBytes  int

	// AllowEmptyPayload turns off the *verdict* half of precommit without
	// touching the buffer itself: a stream that only ever produced metadata is
	// flushed downstream and reported as success instead of failing with
	// ErrNoMeaningfulUpstreamPayload. The buffer still runs, so failover on a
	// late upstream error stays possible either way. A stream that forwarded
	// nothing at all still fails with ErrEmptyUpstreamStream.
	AllowEmptyPayload bool
}

// StreamProcessor unifies all stream handling logic.
type StreamProcessor struct {
	config StreamConfig

	// State
	pendingBuffer  bytes.Buffer
	pendingEvents  int
	payloadWritten bool
	firstToken     bool
	committed      bool
}

// NewStreamProcessor creates a processor from config.
func NewStreamProcessor(config StreamConfig) *StreamProcessor {
	if config.PrecommitPredicate != nil {
		if config.PrecommitMaxEvents <= 0 {
			config.PrecommitMaxEvents = 8
		}
		if config.PrecommitMaxBytes <= 0 {
			config.PrecommitMaxBytes = 64 * 1024
		}
	}
	return &StreamProcessor{
		config:     config,
		firstToken: true,
	}
}

// Run executes the unified stream processing loop.
func (p *StreamProcessor) Run() error {
	// Set SSE response headers
	headers := p.config.Writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")

	// Setup heartbeat ticker
	var heartbeatTicker *time.Ticker
	var heartbeatC <-chan time.Time
	if p.config.HeartbeatInterval > 0 {
		heartbeatTicker = time.NewTicker(p.config.HeartbeatInterval)
		heartbeatC = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}

	// Setup first token timeout
	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if p.firstToken && p.config.FirstTokenTimeout > 0 {
		firstTokenTimer = time.NewTimer(p.config.FirstTokenTimeout)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	// Async read from source — use a derived context so we can unblock on any exit.
	readCtx, readCancel := context.WithCancel(p.config.Context)
	defer readCancel()
	defer p.config.Source.Close()

	type readResult struct {
		data []byte
		err  error
	}
	results := make(chan readResult, 1)
	safe.Go("stream-processor-read", func() {
		defer close(results)
		for {
			data, err := p.config.Source.ReadEvent(readCtx)
			select {
			case results <- readResult{data: data, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	})

	// Main event loop
	for {
		select {
		case <-p.config.Context.Done():
			return p.handleDisconnect()

		case <-firstTokenC:
			return p.handleFirstTokenTimeout()

		case <-heartbeatC:
			if err := p.writeHeartbeat(); err != nil {
				return err
			}

		case r, ok := <-results:
			if !ok {
				// Channel closed, stream ended
				return p.finalize()
			}

			if r.err != nil {
				if r.err == io.EOF {
					return p.finalize()
				}
				if p.config.Context.Err() != nil {
					return p.handleDisconnect()
				}
				return fmt.Errorf("stream read error: %w", r.err)
			}

			if len(r.data) == 0 {
				continue
			}
			if p.config.Observer != nil {
				if err := p.config.Observer.Observe(p.config.Context, r.data); err != nil {
					return fmt.Errorf("stream observe error: %w", err)
				}
			}

			// Transform and write
			if err := p.processEvent(r.data); err != nil {
				return err
			}

			// First token handling
			if p.firstToken && p.payloadWritten {
				p.firstToken = false
				if p.config.OnFirstToken != nil {
					p.config.OnFirstToken()
				}
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}
		}
	}
}

// processEvent transforms and writes a single event.
func (p *StreamProcessor) processEvent(data []byte) error {
	var output []byte
	var err error

	if p.config.Transform != nil {
		output, err = p.config.Transform(p.config.Context, data)
		if err != nil {
			return fmt.Errorf("transform error: %w", err)
		}
		if len(output) == 0 {
			return nil // Skip empty output
		}
	} else {
		output = data // Passthrough
	}

	if p.config.PrecommitPredicate != nil && !p.committed {
		p.pendingBuffer.Write(output)
		p.pendingEvents++
		if !p.config.PrecommitPredicate(data, output) {
			if p.pendingEvents >= p.config.PrecommitMaxEvents || p.pendingBuffer.Len() >= p.config.PrecommitMaxBytes {
				if !p.config.AllowEmptyPayload {
					return fmt.Errorf("%w: events=%d bytes=%d", ErrPrecommitLimitExceeded, p.pendingEvents, p.pendingBuffer.Len())
				}
				// Detection disabled: commit what we buffered instead of failing.
				return p.flushPending()
			}
			return nil
		}
		p.committed = true
		return p.flushPending()
	}

	return p.writeOutput(output)
}

// flushPending commits the precommit buffer downstream and marks the stream as
// committed so later events write straight through.
func (p *StreamProcessor) flushPending() error {
	p.committed = true
	if err := p.writeOutput(p.pendingBuffer.Bytes()); err != nil {
		return err
	}
	p.pendingBuffer.Reset()
	return nil
}

func (p *StreamProcessor) writeOutput(output []byte) error {
	if _, err := p.config.Writer.Write(output); err != nil {
		return fmt.Errorf("write error: %w", err)
	}
	p.payloadWritten = true
	p.config.Writer.Flush()
	return nil
}

// writeHeartbeat sends SSE heartbeat (comment line).
func (p *StreamProcessor) writeHeartbeat() error {
	if _, err := p.config.Writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	p.config.Writer.Flush()
	return nil
}

// handleDisconnect handles context cancellation or timeout.
func (p *StreamProcessor) handleDisconnect() error {
	if p.config.Observer != nil && p.config.Observer.ReachedTerminal() {
		log.Debugf("client disconnected after observer reached terminal event, treating as success")
		return p.finalize()
	}

	err := p.config.Context.Err()
	log.Debugf("client disconnected, stopping stream: written=%t first_token_seen=%t err=%v",
		p.payloadWritten, !p.firstToken, err)

	return err
}

// handleFirstTokenTimeout returns first token timeout error.
func (p *StreamProcessor) handleFirstTokenTimeout() error {
	log.Warnf("first token timeout (%v), switching channel", p.config.FirstTokenTimeout)
	return fmt.Errorf("first token timeout after %v", p.config.FirstTokenTimeout)
}

// finalize completes the stream and calls OnFinish callback.
func (p *StreamProcessor) finalize() error {
	if p.config.Observer != nil {
		if err := p.config.Observer.Finalize(p.config.Context); err != nil {
			return fmt.Errorf("stream observer finalize error: %w", err)
		}
	}
	if p.config.PrecommitPredicate != nil && !p.committed && p.pendingBuffer.Len() > 0 {
		if !p.config.AllowEmptyPayload {
			return ErrNoMeaningfulUpstreamPayload
		}
		// Detection disabled: hand the buffered metadata to the client so the
		// stream completes instead of failing over.
		if err := p.flushPending(); err != nil {
			return err
		}
	}
	if !p.payloadWritten {
		return ErrEmptyUpstreamStream
	}

	log.Debugf("stream end (payload_written=%t)", p.payloadWritten)

	if p.config.OnFinish != nil {
		if err := p.config.OnFinish(p.config.Context); err != nil {
			return err
		}
	}

	return nil
}

// PayloadWritten returns whether any payload has been written to the client.
func (p *StreamProcessor) PayloadWritten() bool {
	return p.payloadWritten
}
