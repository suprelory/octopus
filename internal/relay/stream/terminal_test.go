package stream

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestStreamProcessorClosesAfterCompleteTerminalFrame(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	counted := &contractCountedReader{ReadCloser: reader}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const wire = "data: {\"type\":\"content_block_delta\"}\n\ndata: {\"type\":\"message_stop\"}\n\n"
	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(writer, wire)
		written <- err
		// Deliberately keep the upstream pipe open after the terminal frame.
	}()
	output := newMockStreamWriter()
	finished := 0
	processor := NewStreamProcessor(StreamConfig{
		Source: NewRawSource(counted, 8), Writer: output, Context: ctx,
		Observer: NewIncrementalSSEObserver(1024, map[string]struct{}{"message_stop": {}}, nil),
		OnFinish: func(context.Context) error { finished++; return nil },
	})
	if err := processor.Run(); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil || finished != 1 || counted.closes.Load() != 1 || processor.CleanEOF() {
		t.Fatalf("terminal cleanup: ctx=%v finishes=%d closes=%d EOF=%t", ctx.Err(), finished, counted.closes.Load(), processor.CleanEOF())
	}
	if output.buffer.String() != wire {
		t.Fatal("terminal frame was not fully forwarded")
	}
}
