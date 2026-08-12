package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNonChatStreamEventsRoundTripThroughInternalResponse(t *testing.T) {
	events := []StreamEvent{
		{Kind: StreamEventKindImageDelta, ID: "resp_1", Model: "media-model", Index: 2, Media: &StreamMedia{MediaType: "image/png", Data: "aW1hZ2U=", Format: "png", Index: 3}},
		{Kind: StreamEventKindAudioDelta, ID: "resp_1", Model: "media-model", Index: 2, Media: &StreamMedia{MediaType: "audio/wav", URI: "https://example.test/audio.wav", Format: "wav"}},
		{Kind: StreamEventKindOpaque, ID: "resp_1", Model: "media-model", Index: 2, Opaque: json.RawMessage(`{"type":"response.compaction.delta","delta":"state"}`)},
	}

	response := InternalResponseFromStreamEvents(events)
	if response == nil {
		t.Fatal("InternalResponseFromStreamEvents() returned nil")
	}
	got := StreamEventsFromInternalResponse(response)
	if !reflect.DeepEqual(got, events) {
		t.Fatalf("non-chat event round trip\ngot:  %#v\nwant: %#v", got, events)
	}

	got[0].Media.Data = "changed"
	got[2].Opaque[0] = '['
	if response.NonChatStreamEvents[0].Media.Data != "aW1hZ2U=" || response.NonChatStreamEvents[2].Opaque[0] != '{' {
		t.Fatal("round-trip result shares media or opaque storage with response")
	}
}

func TestHasSemanticStreamEventsRecognizesNonChatPayloads(t *testing.T) {
	tests := []struct {
		name  string
		event StreamEvent
		want  bool
	}{
		{name: "image data", event: StreamEvent{Kind: StreamEventKindImageDelta, Media: &StreamMedia{Data: "AA=="}}, want: true},
		{name: "audio uri", event: StreamEvent{Kind: StreamEventKindAudioDelta, Media: &StreamMedia{URI: "https://example.test/a.wav"}}, want: true},
		{name: "opaque", event: StreamEvent{Kind: StreamEventKindOpaque, Opaque: json.RawMessage(`{"delta":1}`)}, want: true},
		{name: "media metadata only", event: StreamEvent{Kind: StreamEventKindImageDelta, Media: &StreamMedia{Format: "png"}}, want: false},
		{name: "empty opaque", event: StreamEvent{Kind: StreamEventKindOpaque, Opaque: json.RawMessage("  ")}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := HasSemanticStreamEvents([]StreamEvent{test.event}); got != test.want {
				t.Fatalf("HasSemanticStreamEvents() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStreamAggregatorPreservesNonChatEvents(t *testing.T) {
	aggregator := &StreamAggregator{}
	event := StreamEvent{Kind: StreamEventKindImageDelta, Media: &StreamMedia{Data: "AA=="}}
	aggregator.Add(InternalResponseFromStreamEvents([]StreamEvent{event}))
	response := aggregator.BuildAndReset()
	if response == nil || len(response.NonChatStreamEvents) != 1 {
		t.Fatalf("aggregated response = %#v", response)
	}
	if got := StreamEventsFromInternalResponse(response); !reflect.DeepEqual(got, []StreamEvent{event}) {
		t.Fatalf("aggregated events = %#v", got)
	}
}

func TestStreamEventsProjectLegacyImageAndAudioDeltas(t *testing.T) {
	response := &InternalLLMResponse{
		ID:    "chunk_1",
		Model: "multimodal-model",
		Choices: []Choice{{Index: 4, Delta: &Message{
			Images: []MessageContentPart{{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/png;base64,AA=="}}},
			Audio: &struct {
				Data       string `json:"data,omitempty"`
				ExpiresAt  int64  `json:"expires_at,omitempty"`
				ID         string `json:"id,omitempty"`
				Transcript string `json:"transcript,omitempty"`
			}{Data: "YXVkaW8=", ID: "audio_1", Transcript: "hello", ExpiresAt: 42},
		}}},
	}

	events := StreamEventsFromInternalResponse(response)
	if len(events) != 2 || events[0].Kind != StreamEventKindImageDelta || events[1].Kind != StreamEventKindAudioDelta {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Index != 4 || events[0].Media.URI != "data:image/png;base64,AA==" {
		t.Fatalf("image event = %#v", events[0])
	}
	if events[1].Index != 4 || events[1].Media.Data != "YXVkaW8=" || events[1].Media.ID != "audio_1" || events[1].Media.Transcript != "hello" || events[1].Media.ExpiresAt != 42 {
		t.Fatalf("audio event = %#v", events[1])
	}
}
