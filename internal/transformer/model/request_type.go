package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RequestType identifies the semantic operation independently of its wire
// format. APIFormat answers "how is it encoded"; RequestType answers "what
// does it do".
type RequestType string

const (
	RequestTypeUnknown   RequestType = ""
	RequestTypeChat      RequestType = "chat"
	RequestTypeResponses RequestType = "responses"
	RequestTypeEmbedding RequestType = "embedding"
	RequestTypeImages    RequestType = "images"
	RequestTypeRerank    RequestType = "rerank"
)

func (t RequestType) String() string { return string(t) }

func (t RequestType) Valid() bool {
	switch t {
	case RequestTypeChat, RequestTypeResponses, RequestTypeEmbedding, RequestTypeImages, RequestTypeRerank:
		return true
	default:
		return false
	}
}

// RequestOperation is a tagged union. Exactly one payload pointer must be set.
// Common routing fields such as Model remain on InternalLLMRequest; fields that
// vary by endpoint live in the operation payload.
type RequestOperation struct {
	Chat       *ChatOperation       `json:"chat,omitempty"`
	Responses  *ResponsesOperation  `json:"responses,omitempty"`
	Embeddings *EmbeddingsOperation `json:"embeddings,omitempty"`
	Images     *ImagesOperation     `json:"images,omitempty"`
	Rerank     *RerankOperation     `json:"rerank,omitempty"`
}

type ChatOperation struct {
	Messages []Message `json:"messages"`
}

type ResponsesOperation struct {
	Messages           []Message       `json:"messages,omitempty"`
	RawInputItems      json.RawMessage `json:"raw_input_items,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
}

type EmbeddingsOperation struct {
	Input          EmbeddingInput `json:"input"`
	Dimensions     *int64         `json:"dimensions,omitempty"`
	EncodingFormat *string        `json:"encoding_format,omitempty"`
}

type ImagesOperation struct {
	Prompt         string       `json:"prompt,omitempty"`
	Inputs         []ImageInput `json:"inputs,omitempty"`
	Count          *int         `json:"count,omitempty"`
	Size           string       `json:"size,omitempty"`
	Quality        string       `json:"quality,omitempty"`
	ResponseFormat string       `json:"response_format,omitempty"`
}

type ImageInput struct {
	Data      []byte `json:"data,omitempty"`
	URI       string `json:"uri,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Filename  string `json:"filename,omitempty"`
}

type RerankOperation struct {
	Query     string           `json:"query"`
	Documents []RerankDocument `json:"documents"`
	TopN      *int             `json:"top_n,omitempty"`
}

type RerankDocument struct {
	ID       string         `json:"id,omitempty"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (o *RequestOperation) Type() RequestType {
	if o == nil {
		return RequestTypeUnknown
	}
	kind := RequestTypeUnknown
	count := 0
	set := func(present bool, candidate RequestType) {
		if present {
			kind = candidate
			count++
		}
	}
	set(o.Chat != nil, RequestTypeChat)
	set(o.Responses != nil, RequestTypeResponses)
	set(o.Embeddings != nil, RequestTypeEmbedding)
	set(o.Images != nil, RequestTypeImages)
	set(o.Rerank != nil, RequestTypeRerank)
	if count != 1 {
		return RequestTypeUnknown
	}
	return kind
}

func (o *RequestOperation) Validate() error {
	if o == nil {
		return fmt.Errorf("operation is nil")
	}
	kind := o.Type()
	if kind == RequestTypeUnknown {
		return fmt.Errorf("operation must contain exactly one payload")
	}
	switch kind {
	case RequestTypeChat:
		if len(o.Chat.Messages) == 0 {
			return fmt.Errorf("chat operation requires messages")
		}
	case RequestTypeResponses:
		if len(o.Responses.Messages) == 0 && len(o.Responses.RawInputItems) == 0 {
			return fmt.Errorf("responses operation requires messages or raw input items")
		}
	case RequestTypeEmbedding:
		if o.Embeddings.Input.Single == nil && len(o.Embeddings.Input.Multiple) == 0 {
			return fmt.Errorf("embeddings operation requires input")
		}
	case RequestTypeImages:
		if strings.TrimSpace(o.Images.Prompt) == "" && len(o.Images.Inputs) == 0 {
			return fmt.Errorf("images operation requires a prompt or image input")
		}
	case RequestTypeRerank:
		if strings.TrimSpace(o.Rerank.Query) == "" {
			return fmt.Errorf("rerank operation requires a query")
		}
		if len(o.Rerank.Documents) == 0 {
			return fmt.Errorf("rerank operation requires documents")
		}
	}
	return nil
}

// ResolveRequestType returns the explicit operation tag, the compatibility
// RequestType, or an inferred type for legacy callers.
func (r *InternalLLMRequest) ResolveRequestType() RequestType {
	if r == nil {
		return RequestTypeUnknown
	}
	if r.Operation != nil {
		if kind := r.Operation.Type(); kind != RequestTypeUnknown {
			return kind
		}
	}
	if r.RequestType != RequestTypeUnknown {
		return r.RequestType
	}
	return r.inferLegacyRequestType()
}

// NormalizeOperation validates the union and synchronizes its payload with the
// legacy fields used by adapters that have not migrated yet.
func (r *InternalLLMRequest) NormalizeOperation() error { return r.normalizeRequestType() }

func (r *InternalLLMRequest) normalizeRequestType() error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	if r.Operation != nil {
		if err := r.Operation.Validate(); err != nil {
			return err
		}
		kind := r.Operation.Type()
		if r.RequestType != RequestTypeUnknown && r.RequestType != kind {
			return fmt.Errorf("request type %q conflicts with operation type %q", r.RequestType, kind)
		}
		r.RequestType = kind
		r.applyOperationToLegacyFields()
		return nil
	}

	inferred := r.inferLegacyRequestType()
	if r.RequestType == RequestTypeUnknown {
		r.RequestType = inferred
	}
	if !r.RequestType.Valid() {
		return fmt.Errorf("unsupported request type %q", r.RequestType)
	}
	if inferred != RequestTypeUnknown && r.RequestType != inferred {
		return fmt.Errorf("request type %q conflicts with request payload type %q", r.RequestType, inferred)
	}
	r.Operation = r.operationFromLegacyFields()
	if r.Operation == nil {
		return fmt.Errorf("request type %q has no operation payload", r.RequestType)
	}
	return r.Operation.Validate()
}

func (r *InternalLLMRequest) inferLegacyRequestType() RequestType {
	if r.EmbeddingInput != nil {
		return RequestTypeEmbedding
	}
	if len(r.RawInputItems) > 0 {
		return RequestTypeResponses
	}
	if len(r.Messages) > 0 {
		if r.RawAPIFormat == APIFormatOpenAIResponse {
			return RequestTypeResponses
		}
		return RequestTypeChat
	}
	return RequestTypeUnknown
}

func (r *InternalLLMRequest) operationFromLegacyFields() *RequestOperation {
	switch r.RequestType {
	case RequestTypeChat:
		return &RequestOperation{Chat: &ChatOperation{Messages: r.Messages}}
	case RequestTypeResponses:
		return &RequestOperation{Responses: &ResponsesOperation{
			Messages:           r.Messages,
			RawInputItems:      r.RawInputItems,
			PreviousResponseID: r.PreviousResponseID,
		}}
	case RequestTypeEmbedding:
		if r.EmbeddingInput == nil {
			return nil
		}
		return &RequestOperation{Embeddings: &EmbeddingsOperation{
			Input:          *r.EmbeddingInput,
			Dimensions:     r.EmbeddingDimensions,
			EncodingFormat: r.EmbeddingEncodingFormat,
		}}
	default:
		return nil
	}
}

func (r *InternalLLMRequest) applyOperationToLegacyFields() {
	switch r.RequestType {
	case RequestTypeChat:
		r.Messages = r.Operation.Chat.Messages
		r.EmbeddingInput = nil
	case RequestTypeResponses:
		r.Messages = r.Operation.Responses.Messages
		r.RawInputItems = r.Operation.Responses.RawInputItems
		r.PreviousResponseID = r.Operation.Responses.PreviousResponseID
		r.EmbeddingInput = nil
	case RequestTypeEmbedding:
		input := r.Operation.Embeddings.Input
		r.EmbeddingInput = &input
		r.EmbeddingDimensions = r.Operation.Embeddings.Dimensions
		r.EmbeddingEncodingFormat = r.Operation.Embeddings.EncodingFormat
		r.Messages = nil
		r.RawInputItems = nil
	case RequestTypeImages, RequestTypeRerank:
		// These operations have no legacy top-level payload. The tagged operation
		// remains authoritative until their dedicated relays adopt this IR.
	}
}
