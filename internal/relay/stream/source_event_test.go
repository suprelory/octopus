package stream

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestSSESourcePreservesEventType(t *testing.T) {
	source := NewSSESource(io.NopCloser(strings.NewReader("event: message_stop\ndata:\n\n")), 0)
	defer source.Close()

	event, err := source.ReadEventWithType(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "message_stop" || len(event.Data) != 0 {
		t.Fatalf("event = %#v, want message_stop with empty data", event)
	}
}

func TestNormalizeEventDataUsesEnvelopeType(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "empty", data: "", want: `{"type":"message_stop"}`},
		{name: "untyped object", data: `{}`, want: `{"type":"message_stop"}`},
		{name: "null type", data: `{"type":null}`, want: `{"type":"message_stop"}`},
		{name: "typed object", data: `{"type":"other"}`, want: `{"type":"other"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(NormalizeEventData("message_stop", []byte(tt.data)))
			if got != tt.want {
				t.Fatalf("NormalizeEventData() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamProcessorNormalizesTypedSSEEventsForTransform(t *testing.T) {
	source := NewSSESource(io.NopCloser(strings.NewReader("event: message_stop\ndata: {}\n\n")), 0)
	defer source.Close()
	writer := newMockStreamWriter()
	var transformed string
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: context.Background(),
		Transform: func(_ context.Context, data []byte) ([]byte, error) {
			transformed = string(data)
			return data, nil
		},
	})

	if err := processor.Run(); err != nil {
		t.Fatal(err)
	}
	if transformed != `{"type":"message_stop"}` {
		t.Fatalf("transformed event = %q, want envelope type", transformed)
	}
}
