package model

import "testing"

func TestReasoningBlocksByProviderUsesAuthoritativeSignatureSource(t *testing.T) {
	message := Message{ReasoningBlocks: []ReasoningBlock{{
		Kind:      ReasoningBlockKindSignature,
		Provider:  string(SignatureProviderGemini),
		Signature: "openai-signature",
		SignatureSource: &OpaqueSignature{
			Provider: SignatureProviderOpenAI,
			Kind:     OpaqueSignatureKindOpenAIReasoning,
			Value:    "openai-signature",
		},
	}}}

	if blocks := message.ReasoningBlocksByProvider(string(SignatureProviderGemini)); len(blocks) != 0 {
		t.Fatalf("foreign signature classified as Gemini: %+v", blocks)
	}
	if blocks := message.ReasoningBlocksByProvider(string(SignatureProviderOpenAI)); len(blocks) != 1 {
		t.Fatalf("OpenAI signature not found through provenance: %+v", blocks)
	}
}

func TestOpaqueSignatureDoesNotFallbackPastAuthoritativeSource(t *testing.T) {
	block := ReasoningBlock{
		Kind:            ReasoningBlockKindSignature,
		Provider:        string(SignatureProviderAnthropic),
		Signature:       "legacy-signature",
		SignatureSource: &OpaqueSignature{},
	}

	if signature, ok := block.OpaqueSignature(); ok {
		t.Fatalf("empty authoritative source fell back to legacy mirror: %+v", signature)
	}
}

func TestStreamEventRoundTripPreservesInvalidAuthoritativeSource(t *testing.T) {
	block := ReasoningBlock{
		Kind:            ReasoningBlockKindSignature,
		Signature:       "stale-signature",
		SignatureSource: &OpaqueSignature{},
	}
	response := &InternalLLMResponse{Choices: []Choice{{Delta: &Message{ReasoningBlocks: []ReasoningBlock{block}}}}}

	events := StreamEventsFromInternalResponse(response)
	if len(events) != 1 || events[0].Delta == nil || events[0].Delta.SignatureSource == nil {
		t.Fatalf("authoritative source was lost during event projection: %+v", events)
	}
	roundTrip := InternalResponseFromStreamEvents(events)
	if len(roundTrip.Choices) != 1 || roundTrip.Choices[0].Delta == nil {
		t.Fatalf("missing round-trip delta: %+v", roundTrip)
	}
	delta := roundTrip.Choices[0].Delta
	if delta.ReasoningSignatureSource == nil || len(delta.ReasoningBlocks) != 1 || delta.ReasoningBlocks[0].SignatureSource == nil {
		t.Fatalf("authoritative source was lost during event reconstruction: %+v", delta)
	}
	if signature, ok := delta.ReasoningBlocks[0].OpaqueSignature(); ok {
		t.Fatalf("invalid authoritative source became replayable: %+v", signature)
	}
}

func TestStreamEventRoundTripUsesSourceOnlySignatureValue(t *testing.T) {
	source := &OpaqueSignature{
		Provider: SignatureProviderOpenAI,
		Kind:     OpaqueSignatureKindOpenAIReasoning,
		Value:    "source-only-signature",
	}
	response := &InternalLLMResponse{Choices: []Choice{{Delta: &Message{ReasoningBlocks: []ReasoningBlock{{
		Kind:            ReasoningBlockKindSignature,
		SignatureSource: source,
	}}}}}}

	events := StreamEventsFromInternalResponse(response)
	if len(events) != 1 || events[0].Delta == nil || events[0].Delta.Signature != source.Value {
		t.Fatalf("source-only signature was not projected authoritatively: %+v", events)
	}
	roundTrip := InternalResponseFromStreamEvents(events)
	if len(roundTrip.Choices) != 1 || roundTrip.Choices[0].Delta == nil {
		t.Fatalf("missing round-trip delta: %+v", roundTrip)
	}
	delta := roundTrip.Choices[0].Delta
	if delta.ReasoningSignatureSource == nil || delta.ReasoningSignatureSource.Value != source.Value {
		t.Fatalf("source-only signature was not reconstructed: %+v", delta)
	}
	if len(delta.ReasoningBlocks) != 1 || delta.ReasoningBlocks[0].Signature != source.Value {
		t.Fatalf("source-only compatibility mirror was not reconstructed: %+v", delta.ReasoningBlocks)
	}
}
