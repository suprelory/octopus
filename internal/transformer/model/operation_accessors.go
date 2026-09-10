package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// Operation accessors return read-only payload views. Legacy fields are only a
// fallback when no operation has been supplied; an explicit empty operation
// never resurrects stale legacy data. Mutations should use the setters below.
func (r *InternalLLMRequest) ChatPayload() *ChatOperation {
	if r == nil {
		return nil
	}
	if r.Operation != nil {
		return r.Operation.Chat
	}
	if r.ResolveRequestType() != RequestTypeChat {
		return nil
	}
	return &ChatOperation{Messages: r.Messages}
}

func (r *InternalLLMRequest) ResponsesPayload() *ResponsesOperation {
	if r == nil {
		return nil
	}
	if r.Operation != nil {
		return r.Operation.Responses
	}
	if r.ResolveRequestType() != RequestTypeResponses {
		return nil
	}
	return &ResponsesOperation{Messages: r.Messages, RawInputItems: r.RawInputItems, PreviousResponseID: r.PreviousResponseID}
}

func (r *InternalLLMRequest) EmbeddingsPayload() *EmbeddingsOperation {
	if r == nil {
		return nil
	}
	if r.Operation != nil {
		return r.Operation.Embeddings
	}
	if r.EmbeddingInput == nil {
		return nil
	}
	return &EmbeddingsOperation{Input: *r.EmbeddingInput, Dimensions: r.EmbeddingDimensions, EncodingFormat: r.EmbeddingEncodingFormat}
}

func (r *InternalLLMRequest) ImagesPayload() *ImagesOperation {
	if r == nil || r.Operation == nil {
		return nil
	}
	return r.Operation.Images
}

func (r *InternalLLMRequest) RerankPayload() *RerankOperation {
	if r == nil || r.Operation == nil {
		return nil
	}
	return r.Operation.Rerank
}

func (r *InternalLLMRequest) ConversationMessages() []Message {
	if r == nil {
		return nil
	}
	if r.Operation == nil {
		return r.Messages
	}
	if payload := r.ChatPayload(); payload != nil {
		return payload.Messages
	}
	if payload := r.ResponsesPayload(); payload != nil {
		return payload.Messages
	}
	return nil
}

func (r *InternalLLMRequest) SetConversationMessages(messages []Message) {
	if r == nil {
		return
	}
	r.Messages = messages
	if r.Operation != nil {
		if r.Operation.Chat != nil {
			r.Operation.Chat.Messages = messages
		}
		if r.Operation.Responses != nil {
			r.Operation.Responses.Messages = messages
		}
	}
}

// ValidateOperationConsistency enforces the completed adapter migration:
// operation-only and legacy-only callers remain supported, while callers that
// provide both must agree. This check is read-only for builder/planner use.
func (r *InternalLLMRequest) ValidateOperationConsistency() error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	if r.Operation == nil {
		return nil
	}
	if err := r.Operation.Validate(); err != nil {
		return err
	}
	if r.RequestType != RequestTypeUnknown && r.RequestType != r.Operation.Type() {
		return fmt.Errorf("request type %q conflicts with operation type %q", r.RequestType, r.Operation.Type())
	}
	conflict := func(field string) error { return fmt.Errorf("legacy %s conflicts with operation payload", field) }
	if r.Messages != nil && !reflect.DeepEqual(r.Messages, r.ConversationMessages()) {
		return conflict("messages")
	}
	responses := r.ResponsesPayload()
	if r.RawInputItems != nil && (responses == nil || !equalOperationJSON(r.RawInputItems, responses.RawInputItems)) {
		return conflict("raw_input_items")
	}
	if r.PreviousResponseID != nil && (responses == nil || !reflect.DeepEqual(r.PreviousResponseID, responses.PreviousResponseID)) {
		return conflict("previous_response_id")
	}
	embeddings := r.EmbeddingsPayload()
	if r.EmbeddingInput != nil && (embeddings == nil || !reflect.DeepEqual(*r.EmbeddingInput, embeddings.Input)) {
		return conflict("embedding_input")
	}
	if r.EmbeddingDimensions != nil && (embeddings == nil || !reflect.DeepEqual(r.EmbeddingDimensions, embeddings.Dimensions)) {
		return conflict("embedding_dimensions")
	}
	if r.EmbeddingEncodingFormat != nil && (embeddings == nil || !reflect.DeepEqual(r.EmbeddingEncodingFormat, embeddings.EncodingFormat)) {
		return conflict("embedding_encoding_format")
	}
	return nil
}

func equalOperationJSON(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	if !json.Valid(a) || !json.Valid(b) {
		return false
	}
	var left, right any
	ld, rd := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	ld.UseNumber()
	rd.UseNumber()
	return ld.Decode(&left) == nil && rd.Decode(&right) == nil && reflect.DeepEqual(left, right)
}
