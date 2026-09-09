package stream

import (
	"context"
	"io"
	"testing"
)

type testWSReader struct {
	events [][]byte
	index  int
}

func (r *testWSReader) ReadEvent(context.Context) ([]byte, error) {
	if r.index >= len(r.events) {
		return nil, io.EOF
	}
	event := r.events[r.index]
	r.index++
	return event, nil
}

func (*testWSReader) Close() error    { return nil }
func (*testWSReader) CloseWithError() {}
func (*testWSReader) StatusCode() int { return 200 }

func TestWSSourceExtractsEnvelopeMetadata(t *testing.T) {
	source := NewWSSource(&testWSReader{events: [][]byte{
		[]byte(`{"type":"response.created","event_id":"evt-1"}`),
		[]byte(`{"type":"response.completed","id":"evt-2"}`),
	}})

	first, err := source.ReadSourceEvent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.ReadSourceEvent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != "response.created" || first.ID != "evt-1" || first.Sequence != 1 || first.Transport != SourceTransportWebSocket {
		t.Fatalf("first event = %#v, want WebSocket metadata", first)
	}
	if second.Type != "response.completed" || second.ID != "evt-2" || second.Sequence != 2 || second.Transport != SourceTransportWebSocket {
		t.Fatalf("second event = %#v, want WebSocket metadata", second)
	}
}
