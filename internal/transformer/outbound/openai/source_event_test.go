package openai

import (
	"context"
	"reflect"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestResponseTransformSourceEventPrefersEnvelopeType(t *testing.T) {
	outbound := &ResponseOutbound{}
	events, err := outbound.TransformSourceEvent(context.Background(), model.SourceEvent{
		Type:      "response.created",
		Data:      []byte(`{"type":"response.output_text.delta","delta":"must-not-leak","response":{"id":"resp_1","model":"gpt-test"}}`),
		ID:        "wire-1",
		Sequence:  3,
		Transport: model.SourceTransportSSE,
	})
	if err != nil {
		t.Fatalf("TransformSourceEvent: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventKindMessageStart {
		t.Fatalf("events = %+v, want only message_start", events)
	}
}

func TestResponseTransformSourceEventAcceptsTypedLifecycleWithoutJSON(t *testing.T) {
	outbound := &ResponseOutbound{}
	events, err := outbound.TransformSourceEvent(context.Background(), model.SourceEvent{
		Type: "response.completed",
		Data: []byte("not-json"),
	})
	if err != nil {
		t.Fatalf("TransformSourceEvent: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventKindDone {
		t.Fatalf("events = %+v, want done", events)
	}
}

func TestChatTransformSourceEventAcceptsTypedDoneWithoutJSON(t *testing.T) {
	outbound := &ChatOutbound{}
	events, err := outbound.TransformSourceEvent(context.Background(), model.SourceEvent{
		Type:      "done",
		Data:      []byte("not-json"),
		Transport: model.SourceTransportSSE,
	})
	if err != nil {
		t.Fatalf("TransformSourceEvent: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventKindDone {
		t.Fatalf("events = %+v, want done", events)
	}
}

func TestChatByteStreamEventWrapsSourceEventConversion(t *testing.T) {
	outbound := &ChatOutbound{}
	data := []byte(`{"id":"chat_1","choices":[{"index":0,"delta":{"content":"hello"}}]}`)
	want, err := outbound.TransformSourceEvent(context.Background(), model.SourceEvent{Data: data})
	if err != nil {
		t.Fatalf("TransformSourceEvent: %v", err)
	}
	got, err := outbound.TransformStreamEvent(context.Background(), data)
	if err != nil {
		t.Fatalf("TransformStreamEvent: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("byte wrapper = %+v, want %+v", got, want)
	}
}
