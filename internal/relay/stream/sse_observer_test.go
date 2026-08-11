package stream

import (
	"context"
	"reflect"
	"testing"
)

func TestIncrementalSSEObserverHandlesSplitAndMultilineEvents(t *testing.T) {
	type observed struct {
		typ  string
		data string
	}
	var events []observed
	observer := NewIncrementalSSEObserver(1024, map[string]struct{}{"response.completed": {}}, func(_ context.Context, eventType string, data []byte) error {
		events = append(events, observed{typ: eventType, data: string(data)})
		return nil
	})
	for _, chunk := range [][]byte{
		[]byte("event: response.output_text.delta\r\nda"),
		[]byte("ta: {\"type\":\"response.output_text.delta\",\r\n"),
		[]byte("data: \"delta\":\"hello\"}\r\n\r\nevent: response.completed\n"),
		[]byte("data: {\"type\":\"response.completed\"}\n\n"),
	} {
		if err := observer.Observe(context.Background(), chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := observer.Finalize(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []observed{
		{typ: "response.output_text.delta", data: "{\"type\":\"response.output_text.delta\",\n\"delta\":\"hello\"}"},
		{typ: "response.completed", data: "{\"type\":\"response.completed\"}"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if !observer.ReachedTerminal() {
		t.Fatal("terminal event was not detected")
	}
}

func TestIncrementalSSEObserverInfersJSONType(t *testing.T) {
	var gotType string
	observer := NewIncrementalSSEObserver(1024, nil, func(_ context.Context, eventType string, data []byte) error {
		gotType = eventType
		return nil
	})
	if err := observer.Observe(context.Background(), []byte("data: {\"type\":\"message_stop\"}\n\n")); err != nil {
		t.Fatal(err)
	}
	if gotType != "message_stop" {
		t.Fatalf("event type = %q", gotType)
	}
}

func TestIncrementalSSEObserverLimitsIndividualEventNotWholeChunk(t *testing.T) {
	observed := 0
	observer := NewIncrementalSSEObserver(40, nil, func(_ context.Context, _ string, _ []byte) error {
		observed++
		return nil
	})
	chunk := []byte("data: {\"type\":\"one\"}\n\ndata: {\"type\":\"two\"}\n\n")
	if len(chunk) <= 40 {
		t.Fatalf("test chunk must exceed per-event limit, got %d bytes", len(chunk))
	}
	if err := observer.Observe(context.Background(), chunk); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observed != 2 {
		t.Fatalf("observed events = %d, want 2", observed)
	}
}

func TestIncrementalSSEObserverDispatchesCompleteJSONDataLineBeforeBlankLine(t *testing.T) {
	observed := 0
	observer := NewIncrementalSSEObserver(1024, nil, func(_ context.Context, eventType string, data []byte) error {
		observed++
		if eventType != "response.output_text.delta" || string(data) != `{"type":"response.output_text.delta","delta":"ok"}` {
			t.Fatalf("unexpected event: type=%q data=%s", eventType, data)
		}
		return nil
	})
	if err := observer.Observe(context.Background(), []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n")); err != nil {
		t.Fatal(err)
	}
	if observed != 1 {
		t.Fatalf("observed events = %d, want 1 before blank line", observed)
	}
	if err := observer.Observe(context.Background(), []byte("\n")); err != nil {
		t.Fatal(err)
	}
	if observed != 1 {
		t.Fatalf("blank line redispatched event: observed=%d", observed)
	}
}
