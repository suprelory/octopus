package stream

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestSSESourcePreservesEventType(t *testing.T) {
	source := NewSSESource(io.NopCloser(strings.NewReader("id: evt-7\nevent: message_stop\ndata:\n\n")), 0)
	defer source.Close()

	event, err := source.ReadSourceEvent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "message_stop" || len(event.Data) != 0 {
		t.Fatalf("event = %#v, want message_stop with empty data", event)
	}
	if event.ID != "evt-7" || event.Sequence != 1 || event.Transport != SourceTransportSSE {
		t.Fatalf("event metadata = %#v, want id/sequence/transport", event)
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
	source := NewSSESource(io.NopCloser(strings.NewReader("event: message_stop\ndata:\n\n")), 0)
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

func TestStreamProcessorPassesSourceEventToTransform(t *testing.T) {
	source := NewSSESource(io.NopCloser(strings.NewReader("id: evt-3\nevent: response.completed\ndata:\n\n")), 0)
	defer source.Close()
	writer := newMockStreamWriter()
	var got SourceEvent
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: context.Background(),
		TransformEvent: func(_ context.Context, event SourceEvent) ([]byte, error) {
			got = event
			return []byte("event"), nil
		},
	})

	if err := processor.Run(); err != nil {
		t.Fatal(err)
	}
	if got.Type != "response.completed" || got.ID != "evt-3" || got.Sequence != 1 || got.Transport != SourceTransportSSE {
		t.Fatalf("source event = %#v, want complete metadata", got)
	}
}

func TestStreamProcessorDoesNotRewriteEventDataForEventTransform(t *testing.T) {
	const payload = `{"type":"payload-type"}`
	source := NewSSESource(io.NopCloser(strings.NewReader("event: envelope-type\ndata: "+payload+"\n\n")), 0)
	defer source.Close()
	writer := newMockStreamWriter()
	var got SourceEvent
	processor := NewStreamProcessor(StreamConfig{
		Source:  source,
		Writer:  writer,
		Context: context.Background(),
		TransformEvent: func(_ context.Context, event SourceEvent) ([]byte, error) {
			got = event
			return []byte("event"), nil
		},
	})

	if err := processor.Run(); err != nil {
		t.Fatal(err)
	}
	if got.Type != "envelope-type" || string(got.Data) != payload {
		t.Fatalf("source event = %#v, want separate unchanged type and data", got)
	}
}
