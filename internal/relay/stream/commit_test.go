package stream

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type uncertainStreamWriter struct {
	*mockStreamWriter
	write func([]byte) (int, error)
}

func (w *uncertainStreamWriter) Write(data []byte) (int, error) { return w.write(data) }

func TestStreamCommitPrecedesUncertainDelivery(t *testing.T) {
	for _, mode := range []string{"failed", "partial", "short"} {
		t.Run(mode, func(t *testing.T) {
			committed := false
			writer := &uncertainStreamWriter{mockStreamWriter: newMockStreamWriter(), write: func(data []byte) (int, error) {
				if !committed {
					t.Error("write started before the shared commit callback")
				}
				if mode == "short" {
					return len(data) / 2, nil
				}
				if mode == "partial" {
					return len(data) / 2, fmt.Errorf("connection closed")
				}
				return 0, fmt.Errorf("delivery unknown")
			}}
			processor := NewStreamProcessor(StreamConfig{
				Source: newMockStreamSource([][]byte{[]byte("payload")}), Writer: writer, Context: context.Background(),
				OnCommit: func() { committed = true },
			})
			if err := processor.Run(); !errors.Is(err, ErrDownstreamWrite) {
				t.Fatalf("write error must retain its origin: %v", err)
			}
			if !committed || !processor.PayloadWritten() {
				t.Fatal("uncertain delivery must prohibit retry")
			}
		})
	}
}

func TestStreamHeartbeatDoesNotCommitPayload(t *testing.T) {
	committed := false
	processor := NewStreamProcessor(StreamConfig{
		Writer: newMockStreamWriter(), Context: context.Background(), OnCommit: func() { committed = true },
	})
	if err := processor.writeHeartbeat(); err != nil {
		t.Fatal(err)
	}
	if committed || processor.PayloadWritten() {
		t.Fatal("heartbeat must leave failover available")
	}
}
