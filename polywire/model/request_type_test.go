package model

import (
	"strings"
	"testing"
)

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

func TestRequestOperationTaggedUnion(t *testing.T) {
	text := "hello"
	tests := []struct {
		name  string
		type_ RequestType
		op    *RequestOperation
	}{
		{"chat", RequestTypeChat, &RequestOperation{Chat: &ChatOperation{Messages: []Message{{Role: "user", Content: MessageContent{Content: &text}}}}}},
		{"responses", RequestTypeResponses, &RequestOperation{Responses: &ResponsesOperation{Messages: []Message{{Role: "user", Content: MessageContent{Content: &text}}}}}},
		{"embeddings", RequestTypeEmbedding, &RequestOperation{Embeddings: &EmbeddingsOperation{Input: EmbeddingInput{Single: &text}}}},
		{"images", RequestTypeImages, &RequestOperation{Images: &ImagesOperation{Prompt: "draw an octopus"}}},
		{"rerank", RequestTypeRerank, &RequestOperation{Rerank: &RerankOperation{Query: "q", Documents: []RerankDocument{{Text: "d"}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InternalLLMRequest{Model: "m", Operation: tt.op}
			if err := req.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if req.RequestType != tt.type_ || req.ResolveRequestType() != tt.type_ {
				t.Fatalf("type = %q/%q, want %q", req.RequestType, req.ResolveRequestType(), tt.type_)
			}
			if tt.type_ == RequestTypeImages && !req.IsImageGenerationRequest() {
				t.Fatal("images operation was not recognized as image generation")
			}
		})
	}
}

func TestRequestOperationRejectsAmbiguousAndConflictingPayloads(t *testing.T) {
	text := "hello"
	message := Message{Role: "user", Content: MessageContent{Content: &text}}
	tests := []struct {
		name string
		req  *InternalLLMRequest
		want string
	}{
		{
			name: "ambiguous",
			req: &InternalLLMRequest{Model: "m", Operation: &RequestOperation{
				Chat:      &ChatOperation{Messages: []Message{message}},
				Responses: &ResponsesOperation{Messages: []Message{message}},
			}},
			want: "exactly one",
		},
		{
			name: "conflicting tag",
			req: &InternalLLMRequest{
				Model:       "m",
				RequestType: RequestTypeEmbedding,
				Operation:   &RequestOperation{Chat: &ChatOperation{Messages: []Message{message}}},
			},
			want: "conflicts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
