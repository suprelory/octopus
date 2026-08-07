package model

import "fmt"

// RequestType identifies the semantic operation independently of its wire
// format. APIFormat answers "how is it encoded"; RequestType answers "what
// does it do".
type RequestType string

const (
	RequestTypeUnknown   RequestType = ""
	RequestTypeChat      RequestType = "chat"
	RequestTypeEmbedding RequestType = "embedding"
)

func (t RequestType) String() string {
	return string(t)
}

func (t RequestType) Valid() bool {
	switch t {
	case RequestTypeChat, RequestTypeEmbedding:
		return true
	default:
		return false
	}
}

// ResolveRequestType returns the explicit request type, or infers it for
// callers that construct InternalLLMRequest values directly.
func (r *InternalLLMRequest) ResolveRequestType() RequestType {
	if r == nil {
		return RequestTypeUnknown
	}
	if r.RequestType != RequestTypeUnknown {
		return r.RequestType
	}
	if r.EmbeddingInput != nil {
		return RequestTypeEmbedding
	}
	if len(r.Messages) > 0 || (r.RawAPIFormat == APIFormatOpenAIResponse && len(r.RawInputItems) > 0) {
		return RequestTypeChat
	}
	return RequestTypeUnknown
}

func (r *InternalLLMRequest) normalizeRequestType() error {
	hasEmbeddingPayload := r.EmbeddingInput != nil
	hasChatPayload := len(r.Messages) > 0 || (r.RawAPIFormat == APIFormatOpenAIResponse && len(r.RawInputItems) > 0)
	if hasEmbeddingPayload && hasChatPayload {
		return fmt.Errorf("cannot specify both messages and input")
	}

	inferred := RequestTypeUnknown
	if hasEmbeddingPayload {
		inferred = RequestTypeEmbedding
	} else if hasChatPayload {
		inferred = RequestTypeChat
	}

	if r.RequestType == RequestTypeUnknown {
		r.RequestType = inferred
		return nil
	}
	if !r.RequestType.Valid() {
		return fmt.Errorf("unsupported request type %q", r.RequestType)
	}
	if inferred != RequestTypeUnknown && r.RequestType != inferred {
		return fmt.Errorf("request type %q conflicts with request payload type %q", r.RequestType, inferred)
	}
	return nil
}
