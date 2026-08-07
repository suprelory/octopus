package model

import "testing"

func TestRequestTypeInferenceAndValidation(t *testing.T) {
	text := "hello"
	chat := &InternalLLMRequest{Model: "m", Messages: []Message{{Role: "user", Content: MessageContent{Content: &text}}}}
	if err := chat.Validate(); err != nil {
		t.Fatalf("chat Validate: %v", err)
	}
	if chat.RequestType != RequestTypeChat {
		t.Fatalf("chat RequestType = %q", chat.RequestType)
	}

	embedding := &InternalLLMRequest{Model: "m", EmbeddingInput: &EmbeddingInput{Single: &text}}
	if err := embedding.Validate(); err != nil {
		t.Fatalf("embedding Validate: %v", err)
	}
	if embedding.RequestType != RequestTypeEmbedding {
		t.Fatalf("embedding RequestType = %q", embedding.RequestType)
	}

	mismatch := &InternalLLMRequest{
		Model:        "m",
		RequestType:  RequestTypeEmbedding,
		Messages:     chat.Messages,
		RawAPIFormat: APIFormatOpenAIChatCompletion,
	}
	if err := mismatch.Validate(); err == nil {
		t.Fatal("expected explicit request type mismatch to fail validation")
	}
}
