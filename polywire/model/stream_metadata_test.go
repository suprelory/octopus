package model

import "testing"

func TestCanonicalBatchPreservesAudioAndMixedContentOrder(t *testing.T) {
	result := InternalResponseFromStreamEvents([]StreamEvent{
		{Kind: StreamEventKindTextDelta, Delta: &StreamDelta{Text: "before"}},
		{Kind: StreamEventKindImageDelta, Media: &StreamMedia{Placement: "content", URI: "data:image/png;base64,AA=="}},
		{Kind: StreamEventKindTextDelta, Delta: &StreamDelta{Text: "after"}},
		{Kind: StreamEventKindAudioDelta, Media: &StreamMedia{Placement: "audio", ID: "audio", Data: "AA", Transcript: "one"}},
		{Kind: StreamEventKindAudioDelta, Media: &StreamMedia{Placement: "audio", Data: "BB", Transcript: "two"}},
	})
	delta := result.Choices[0].Delta
	parts := delta.Content.MultipleContent
	if len(parts) != 3 || *parts[0].Text != "before" || parts[1].ImageURL == nil || *parts[2].Text != "after" || delta.Audio.ID != "audio" || delta.Audio.Data != "AABB" || delta.Audio.Transcript != "onetwo" {
		t.Fatalf("batch projection lost content: %+v", delta)
	}
}

func TestCanonicalStreamPreservesResponseAndChoiceMetadata(t *testing.T) {
	text := "grounded"
	source := &InternalLLMResponse{ID: "r", Model: "m", Created: 1234, SystemFingerprint: "fp", ServiceTier: "priority", Choices: []Choice{{Index: 2, Delta: &Message{Content: MessageContent{Content: &text}}, Logprobs: &LogprobsContent{}, Grounding: &GroundingInfo{SearchQueries: []string{"query"}}}}}
	result := InternalResponseFromStreamEvents(StreamEventsFromInternalResponse(source))
	if result.Created != source.Created || result.SystemFingerprint != source.SystemFingerprint || result.ServiceTier != source.ServiceTier || len(result.Choices) != 1 || result.Choices[0].Index != 2 || result.Choices[0].Logprobs == nil || result.Choices[0].Grounding == nil || result.Choices[0].Grounding.SearchQueries[0] != "query" {
		t.Fatalf("metadata lost: %+v", result)
	}
}
