package model

import (
	"encoding/json"
)

// Response is the unified response model.
// To reduce the work of converting the response, we use the OpenAI response format.
// And other llm provider should convert the response to this format.
// NOTE: the OpenAI stream and non-stream response reuse same struct.
type InternalLLMResponse struct {
	ID string `json:"id"`

	// RawResponsesOutputItems preserves exact OpenAI Responses output items when available.
	// It is an internal helper field for exact replay reconstruction and is not part of API output.
	RawResponsesOutputItems json.RawMessage `json:"-"`

	// NonChatStreamEvents preserves canonical media and opaque events while a
	// stream is projected through the legacy response/chunk aggregation model.
	// Protocol adapters consume these events explicitly; they are never emitted
	// by the default OpenAI-compatible JSON serializer.
	NonChatStreamEvents []StreamEvent `json:"-"`

	// A list of chat completion choices. Can be more than one if `n` is greater
	// than 1.
	// For chat completion responses, this field is required.
	// For embedding responses, this field should be empty and EmbeddingData should be used instead.
	Choices []Choice `json:"choices,omitempty"`

	// Embedding API 响应（与 Choices 互斥）
	// EmbeddingData is the list of embedding objects.
	// For embedding responses, this field is required.
	// For chat completion responses, this field should be empty.
	EmbeddingData []EmbeddingObject `json:"embedding_data,omitempty"`

	// Object is the type of the response.
	// e.g. "chat.completion", "chat.completion.chunk", "list"
	Object string `json:"object"`

	// Created is the timestamp of when the response was created.
	Created int64 `json:"created"`

	// Model is the model used to generate the response.
	Model string `json:"model"`

	// An optional field that will only be present when you set stream_options: {"include_usage": true} in your request.
	// When present, it contains a null value except for the last chunk which contains the token usage statistics
	// for the entire request.
	Usage *Usage `json:"usage,omitempty"`

	// This fingerprint represents the backend configuration that the model runs with.
	//
	// Can be used in conjunction with the `seed` request parameter to understand when
	// backend changes have been made that might impact determinism.
	SystemFingerprint string `json:"system_fingerprint,omitempty"`

	// ServiceTier is the service tier of the response.
	// e.g. "free", "standard", "premium"
	ServiceTier string `json:"service_tier,omitempty"`

	// Error is the error information, will present if request to llm service failed with status >= 400.
	Error *ResponseError `json:"error,omitempty"`
}

func (r *InternalLLMResponse) ClearHelpFields() {
	for i, choice := range r.Choices {
		if choice.Message != nil {
			choice.Message.ClearHelpFields()
		}

		if choice.Delta != nil {
			choice.Delta.ClearHelpFields()
		}

		r.Choices[i] = choice
	}
}

// IsEmbeddingResponse returns true if this is an embedding response.
func (r *InternalLLMResponse) IsEmbeddingResponse() bool {
	return len(r.EmbeddingData) > 0
}

// IsChatResponse returns true if this is a chat completion response.
func (r *InternalLLMResponse) IsChatResponse() bool {
	return len(r.Choices) > 0
}

// Choice represents a choice in the response.
// Choice represents a choice in the response.
type Choice struct {
	// Index is the index of the choice in the list of choices.
	Index int `json:"index"`

	// Message is the message content, will present if stream is false
	Message *Message `json:"message,omitempty"`

	// Delta is the stream event content, will present if stream is true
	Delta *Message `json:"delta,omitempty"`

	// FinishReason is the reason the model stopped generating tokens.
	// e.g. "stop", "length", "content_filter", "function_call", "tool_calls"
	FinishReason *string `json:"finish_reason,omitempty"`

	// StopSequence is the matched custom stop string reported by Anthropic when
	// FinishReason is "stop_sequence". Carried through so the originating
	// inbound can round-trip the value.
	StopSequence *string `json:"stop_sequence,omitempty"`

	Logprobs *LogprobsContent `json:"logprobs,omitempty"`

	// Grounding carries search / retrieval metadata surfaced by providers
	// that support grounded generation (Gemini googleSearch tool, future
	// Anthropic web_search result consolidation). Non-grounded responses
	// leave this nil. The json:"-" tag keeps the field off the default
	// OpenAI-compatible wire path; inbound transformers that understand the
	// structure (Gemini inbound, Anthropic inbound) expose it via their
	// provider-native shape. G-H10.
	Grounding *GroundingInfo `json:"-"`

	// Citations carries inline citation spans (start/end offsets +
	// source URLs / licenses) emitted by Gemini's citationMetadata or
	// equivalent. json:"-" for the same reason as Grounding. G-H10.
	Citations []Citation `json:"-"`

	// URLContext carries per-URL retrieval status for Gemini's urlContext
	// tool (whether each URL was fetched successfully). G-H10.
	URLContext *URLContextInfo `json:"-"`

	// SafetyRatings carries per-category safety evaluation data for
	// providers that surface it (Gemini safetyRatings on the candidate and
	// on promptFeedback). json:"-" so the field doesn't pollute the
	// OpenAI-compatible wire body. G-M9.
	SafetyRatings []SafetyRating `json:"-"`
}

// GroundingInfo captures cross-provider search / retrieval grounding data.
// Fields are populated best-effort from the provider's native payload —
// callers should treat missing fields as "not surfaced by this provider"
// rather than "empty".
type GroundingInfo struct {
	// SearchQueries holds the queries the provider actually issued. Gemini
	// surfaces this in groundingMetadata.webSearchQueries.
	SearchQueries []string `json:"search_queries,omitempty"`

	// Sources is the list of upstream documents / web pages the response
	// was grounded on. For Gemini this comes from groundingChunks; for
	// Anthropic's web_search_tool_result the URLs are folded here too.
	Sources []GroundingSource `json:"sources,omitempty"`

	// Supports ties spans of the generated text to the indices in Sources
	// that supported that span. Empty when the provider did not surface
	// span-level attributions.
	Supports []GroundingSupport `json:"supports,omitempty"`

	// SearchEntryPointHTML is the provider-rendered "search entry point"
	// HTML snippet (Gemini surfaces this so UIs can display the required
	// Google Search suggestion chip). Empty for providers that don't
	// supply an entry point.
	SearchEntryPointHTML string `json:"search_entry_point_html,omitempty"`
}

// GroundingSource identifies a single upstream document / web page that a
// grounded response drew on.
type GroundingSource struct {
	URI     string `json:"uri,omitempty"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// GroundingSupport ties a span of the generated text to the source indices
// (into GroundingInfo.Sources) that supported that span. ConfidenceScores
// mirrors Gemini's per-chunk confidence floats; providers that don't surface
// confidence leave it nil.
type GroundingSupport struct {
	// SegmentStartIndex / SegmentEndIndex are byte offsets into the
	// generated text. Gemini sometimes omits startIndex when the segment
	// starts at 0 — callers should default to 0 in that case.
	SegmentStartIndex int `json:"segment_start_index,omitempty"`
	SegmentEndIndex   int `json:"segment_end_index,omitempty"`
	// SegmentText is the literal text this support covers (redundant with
	// the offsets but cheaper for callers that want to display the span).
	SegmentText string `json:"segment_text,omitempty"`
	// SourceIndices points into GroundingInfo.Sources.
	SourceIndices    []int     `json:"source_indices,omitempty"`
	ConfidenceScores []float64 `json:"confidence_scores,omitempty"`
}

// Citation is the inline citation span emitted by providers that generate
// attributed output (Gemini citationMetadata). StartIndex / EndIndex are
// byte offsets into the generated text. License is optional (Gemini
// sometimes surfaces the license associated with the cited source).
type Citation struct {
	StartIndex int    `json:"start_index,omitempty"`
	EndIndex   int    `json:"end_index,omitempty"`
	URI        string `json:"uri,omitempty"`
	Title      string `json:"title,omitempty"`
	License    string `json:"license,omitempty"`
}

// URLContextInfo carries per-URL retrieval status for Gemini's urlContext
// tool: whether the URL was successfully fetched and, if not, why.
type URLContextInfo struct {
	URLs []URLContextEntry `json:"urls,omitempty"`
}

// URLContextEntry is a single URL's retrieval status.
type URLContextEntry struct {
	URL    string `json:"url,omitempty"`
	Status string `json:"status,omitempty"` // e.g. URL_RETRIEVAL_STATUS_SUCCESS / FAILED / INVALID_URL
}

// SafetyRating mirrors a provider's per-category content safety evaluation.
// Gemini surfaces these on both candidates and promptFeedback; Anthropic's
// refusal responses carry a coarser variant that can flow through the same
// shape. G-M9.
type SafetyRating struct {
	Category    string `json:"category,omitempty"`
	Probability string `json:"probability,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
}

// LogprobsContent represents logprobs information.
type LogprobsContent struct {
	Content []TokenLogprob `json:"content"`
}

// TokenLogprob represents logprob for a token.
type TokenLogprob struct {
	Token       string       `json:"token"`
	Logprob     float64      `json:"logprob"`
	Bytes       []int        `json:"bytes,omitempty"`
	TopLogprobs []TopLogprob `json:"top_logprobs,omitempty"`
}

// TopLogprob represents top alternative tokens.
type TopLogprob struct {
	Token   string  `json:"token"`
	Logprob float64 `json:"logprob"`
	Bytes   []int   `json:"bytes,omitempty"`
}

type ResponseMeta struct {
	ID    string `json:"id"`
	Usage *Usage `json:"usage"`
}
