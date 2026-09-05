package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Request is the unified llm request model for AxonHub, to keep compatibility with major app and framework.
// It choose to base on the OpenAI chat completion request, but add some extra fields to support more features.
type InternalLLMRequest struct {
	// Stable cross-provider IR fields.
	// These carry the normalized request semantics shared by multiple providers.
	// New provider-specific features should not be added here unless they are
	// truly cross-provider concepts.

	// RequestType identifies the semantic operation independently of the
	// provider API format. It is internal-only and inferred for legacy callers.
	RequestType RequestType `json:"-"`

	// Operation is the tagged operation payload. It is the preferred API for
	// new endpoints; legacy top-level payload fields remain populated while
	// adapters migrate to operation-specific request types.
	Operation *RequestOperation `json:"-"`

	// Messages is a list of messages to send to the llm model.
	// For chat completion requests, this field is required.
	// For embedding requests, this field should be empty and Input should be used instead.
	Messages []Message `json:"messages,omitempty" validator:"required,min=1"`

	// Embedding API 参数（与 Messages 互斥）
	// EmbeddingInput is the text or texts to get embeddings for.
	// For embedding requests, this field is required.
	// For chat completion requests, this field should be empty.
	EmbeddingInput *EmbeddingInput `json:"embedding_input,omitempty"` // string or string[]
	// EmbeddingDimensions is the number of dimensions for the embedding output.
	// Only supported for certain embedding models.
	EmbeddingDimensions *int64 `json:"embedding_dimensions,omitempty"`
	// EmbeddingEncodingFormat is the format of the embedding output.
	// Can be "float" or "base64". Defaults to "float".
	EmbeddingEncodingFormat *string `json:"embedding_encoding_format,omitempty"`

	// Model is the model ID used to generate the response.
	Model string `json:"model" validator:"required"`

	// Number between -2.0 and 2.0. Positive values penalize new tokens based on
	// their existing frequency in the text so far, decreasing the model's likelihood
	// to repeat the same line verbatim.
	//
	// See [OpenAI's
	// documentation](https://platform.openai.com/docs/api-reference/parameter-details)
	// for more information.
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`

	// Whether to return log probabilities of the output tokens or not. If true,
	// returns the log probabilities of each output token returned in the `content` of
	// `message`.
	Logprobs *bool `json:"logprobs,omitempty"`

	// An upper bound for the number of tokens that can be generated for a completion,
	// including visible output tokens and
	// [reasoning tokens](https://platform.openai.com/docs/guides/reasoning).
	MaxCompletionTokens *int64 `json:"max_completion_tokens,omitempty"`

	// The maximum number of [tokens](/tokenizer) that can be generated in the chat
	// completion. This value can be used to control
	// [costs](https://openai.com/api/pricing/) for text generated via API.
	//
	// This value is now deprecated in favor of `max_completion_tokens`, and is not
	// compatible with
	// [o-series models](https://platform.openai.com/docs/guides/reasoning).
	MaxTokens *int64 `json:"max_tokens,omitempty"`

	// How many chat completion choices to generate for each input message. Note that
	// you will be charged based on the number of generated tokens across all of the
	// choices. Keep `n` as `1` to minimize costs.
	// NOTE: Not supported, always 1.
	// N *int64 `json:"n,omitempty"`

	// Number between -2.0 and 2.0. Positive values penalize new tokens based on
	// whether they appear in the text so far, increasing the model's likelihood to
	// talk about new topics.
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`

	// This feature is in Beta. If specified, our system will make a best effort to
	// sample deterministically, such that repeated requests with the same `seed` and
	// parameters should return the same result. Determinism is not guaranteed, and you
	// should refer to the `system_fingerprint` response parameter to monitor changes
	// in the backend.
	Seed *int64 `json:"seed,omitempty"`

	// Whether or not to store the output of this chat completion request for use in
	// our [model distillation](https://platform.openai.com/docs/guides/distillation)
	// or [evals](https://platform.openai.com/docs/guides/evals) products.
	//
	// Supports text and image inputs. Note: image inputs over 10MB will be dropped.
	Store *bool `json:"store,omitzero"`

	// What sampling temperature to use, between 0 and 2. Higher values like 0.8 will
	// make the output more random, while lower values like 0.2 will make it more
	// focused and deterministic. We generally recommend altering this or `top_p` but
	// not both.
	Temperature *float64 `json:"temperature,omitempty"`

	// An integer between 0 and 20 specifying the number of most likely tokens to
	// return at each token position, each with an associated log probability.
	// `logprobs` must be set to `true` if this parameter is used.
	TopLogprobs *int64 `json:"top_logprobs,omitzero"`

	// An alternative to sampling with temperature, called nucleus sampling, where the
	// model considers the results of the tokens with top_p probability mass. So 0.1
	// means only the tokens comprising the top 10% probability mass are considered.
	//
	// We generally recommend altering this or `temperature` but not both.
	TopP *float64 `json:"top_p,omitempty"`

	// TopK samples from the top K options for each subsequent token (Anthropic,
	// Gemini, some OpenAI-compatible models such as Qwen). OpenAI Chat
	// Completions itself does not accept top_k — Chat outbound simply does not
	// forward this field. A-H3.
	TopK *int64 `json:"top_k,omitempty"`

	// Used by OpenAI to cache responses for similar requests to optimize your cache
	// hit rates. Replaces the `user` field. The OpenAI spec defines this as a
	// string up to 128 characters (stable identifier, e.g. hash(userID) or
	// session ID). Using *bool here silently corrupted requests that set
	// this field; the type is corrected to *string as of O-C4.
	// [Learn more](https://platform.openai.com/docs/guides/prompt-caching).
	PromptCacheKey *string `json:"prompt_cache_key,omitempty"`

	// A stable identifier used to help detect users of your application that may be
	// violating OpenAI's usage policies. The IDs should be a string that uniquely
	// identifies each user. We recommend hashing their username or email address, in
	// order to avoid sending us any identifying information.
	// [Learn more](https://platform.openai.com/docs/guides/safety-best-practices#safety-identifiers).
	SafetyIdentifier *string `json:"safety_identifier,omitzero"`

	// This field is being replaced by `safety_identifier` and `prompt_cache_key`. Use
	// `prompt_cache_key` instead to maintain caching optimizations. A stable
	// identifier for your end-users. Used to boost cache hit rates by better bucketing
	// similar requests and to help OpenAI detect and prevent abuse.
	// [Learn more](https://platform.openai.com/docs/guides/safety-best-practices#safety-identifiers).
	User *string `json:"user,omitempty"`

	// Verbosity controls the detail level of GPT-5 family completions
	// ("low" | "medium" | "high"). Only forwarded when explicitly set;
	// undefined lets the upstream apply its own default.
	// [Learn more](https://platform.openai.com/docs/api-reference/chat/create#chat-create-verbosity).
	Verbosity *string `json:"verbosity,omitempty"`

	// Prediction is the OpenAI "predicted outputs" payload used to bias the
	// decoder when the caller already knows a large portion of the expected
	// output (typical for code / doc edits). Kept as RawMessage so we pass the
	// upstream schema through verbatim rather than risk coercing it.
	// [Learn more](https://platform.openai.com/docs/guides/predicted-outputs).
	Prediction json.RawMessage `json:"prediction,omitempty"`

	// WebSearchOptions configures the built-in `web_search` tool for Chat
	// Completions (search_context_size, user_location, ...). Treated as an
	// opaque passthrough to avoid drifting from the rapidly-evolving schema.
	// [Learn more](https://platform.openai.com/docs/guides/tools-web-search).
	WebSearchOptions json.RawMessage `json:"web_search_options,omitempty"`

	// Parameters for audio output. Required when audio output is requested with
	// `modalities: ["audio"]`.
	// [Learn more](https://platform.openai.com/docs/guides/audio).
	// TODO
	// Audio ChatCompletionAudioParam `json:"audio,omitzero"`

	// Modify the likelihood of specified tokens appearing in the completion.
	//
	// Accepts a JSON object that maps tokens (specified by their token ID in the
	// tokenizer) to an associated bias value from -100 to 100. Mathematically, the
	// bias is added to the logits generated by the model prior to sampling. The exact
	// effect will vary per model, but values between -1 and 1 should decrease or
	// increase likelihood of selection; values like -100 or 100 should result in a ban
	// or exclusive selection of the relevant token.
	LogitBias map[string]int64 `json:"logit_bias,omitempty"`

	// Set of 16 key-value pairs that can be attached to an object. This can be useful
	// for storing additional information about the object in a structured format, and
	// querying for objects via API or the dashboard.
	//
	// Keys are strings with a maximum length of 64 characters. Values are strings with
	// a maximum length of 512 characters.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Output types that you would like the model to generate. Most models are capable
	// of generating text, which is the default:
	//
	// `["text"]`
	// To generate audio, you can use:
	// `["text", "audio"]`
	// To generate image, you can use:
	// `["text", "image"]`
	// Please note that not all models support audio and image generation.
	// Any of "text", "audio", "image".
	Modalities []string `json:"modalities,omitempty"`

	Audio *struct {
		Format string `json:"format,omitempty"`
		Voice  string `json:"voice,omitempty"`
	} `json:"audio,omitempty"`

	// Controls effort on reasoning for reasoning models. It can be set to "low", "medium", or "high".
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// Reasoning budget for reasoning models.
	// Help fields， will not be sent to the llm service.
	ReasoningBudget *int64 `json:"-"`

	// AdaptiveThinking indicates the client requested adaptive thinking mode.
	// Help field, will not be sent to the llm service.
	AdaptiveThinking bool `json:"-"`

	// ThinkingDisplay passes through the Anthropic `thinking.display` value
	// ("summarized" | "omitted"). Help field, will not be sent directly to the llm service.
	ThinkingDisplay string `json:"-"`

	// EnableThinking is used by Alibaba Qwen models to enable thinking/reasoning output.
	EnableThinking *bool `json:"enable_thinking,omitempty"`

	// Thinking is used by DeepSeek models to control thinking/reasoning mode.
	// Valid values: {"type": "enabled"} or {"type": "disabled"}
	// Ref: https://api-docs.deepseek.com/guides/thinking_mode
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Specifies the processing type used for serving the request.
	ServiceTier *string `json:"service_tier,omitempty"`

	// Truncation is the OpenAI Responses API truncation strategy. Valid values
	// are "auto" and "disabled". Carried through so it can be echoed back in
	// response.completed (O-H5).
	Truncation *string `json:"truncation,omitempty"`

	// Not supported with latest reasoning models `o3` and `o4-mini`.
	//
	// Up to 4 sequences where the API will stop generating further tokens. The
	// returned text will not contain the stop sequence.
	Stop *Stop `json:"stop,omitempty"` // string or []string

	Stream        *bool          `json:"stream,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// Static predicted output content, such as the content of a text file that is
	// being regenerated.
	// TODO
	// Prediction ChatCompletionPredictionContentParam `json:"prediction,omitempty"`

	// Whether to enable
	// [parallel function calling](https://platform.openai.com/docs/guides/function-calling#configuring-parallel-function-calling)
	// during tool use.
	ParallelToolCalls *bool       `json:"parallel_tool_calls,omitempty"`
	Tools             []Tool      `json:"tools,omitempty"`
	ToolChoice        *ToolChoice `json:"tool_choice,omitempty"`

	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// Help fields， will not be sent to the llm service.

	// Internal helper fields.
	// These values help preserve transport details, replay state, and format
	// fidelity across the relay pipeline. They are runtime-only and are not sent
	// directly to upstream providers.
	// ExtraBody is helpful to extend the request for different providers.
	// It will not be sent to the OpenAI server.
	ExtraBody json.RawMessage `json:"extra_body,omitempty"`

	// RawRequest is the raw request from the client.
	RawRequest []byte `json:"-"`

	// RawAPIFormat is the original format of the request.
	// e.g. the request from the chat/completions endpoint is in the openai/chat_completion format.
	RawAPIFormat APIFormat `json:"-"`

	// TransformerMetadata stores transformer-specific metadata for preserving format during transformations.
	// This is a help field and will not be sent to the llm service.
	TransformerMetadata map[string]string `json:"-"`

	// TransformOptions stores transformer-specific options for preserving request format.
	// This is a help field and will not be sent to the llm service.
	TransformOptions TransformOptions `json:"-"`

	// Include specifies additional output data to include in the model response.
	// This is a help field and will not be sent to the llm service.
	// e.g., "file_search_call.results", "message.input_image.image_url", "reasoning.encrypted_content"
	Include []string `json:"-"`

	// Provider-specific pass-through fields.
	// These preserve wire-level provider capabilities that do not belong in the
	// stable IR. Prefer exposing new provider-specific behavior through
	// ProviderExtensions and provider-specific accessors instead of growing more
	// top-level passthrough fields.
	PreviousResponseID       *string         `json:"-"`
	Background               *bool           `json:"-"`
	Prompt                   json.RawMessage `json:"-"`
	ResponsesPromptCacheKey  *string         `json:"-"`
	PromptCacheRetention     *string         `json:"-"`
	MaxToolCalls             *int64          `json:"-"`
	Conversation             json.RawMessage `json:"-"`
	ContextManagement        json.RawMessage `json:"-"`
	ResponsesStreamOptions   json.RawMessage `json:"-"`
	ReasoningSummary         *string         `json:"-"`
	ReasoningGenerateSummary *string         `json:"-"`
	// RawInputItems preserves original Responses input items when the request cannot
	// be losslessly normalized into Messages. Relay replay/exact-replay depends on
	// this top-level field directly and it remains the authoritative runtime
	// source even when ProviderExtensions also carries a compatibility mirror.
	RawInputItems json.RawMessage `json:"-"`

	// ProviderExtensions stores provider-specific request hints that are not part
	// of the core cross-provider request model. It is internal-only.
	ProviderExtensions *ProviderExtensions `json:"-"`

	// Presence records fields explicitly supplied by the inbound payload. It
	// distinguishes absent from explicit null/empty values without widening the
	// canonical field types or serializing runtime metadata upstream.
	Presence map[string]FieldPresence `json:"-"`

	// EstimatedInputTokens is the input token count the inbound transformer
	// computed while parsing the client request (system blocks, messages,
	// content blocks, tool schemas). Runtime-only help field: it is not sent
	// upstream, but lets retry attempts re-seed inbound adapter state without
	// re-parsing (and re-counting) the unchanged request body.
	EstimatedInputTokens int64 `json:"-"`

	// Query stores the original query parameters from the inbound request.
	// This is a help field and will not be sent to the llm service.
	Query url.Values `json:"-"`
}

func (r *InternalLLMRequest) Validate() error {
	if err := r.normalizeRequestType(); err != nil {
		return err
	}
	if r.Model == "" {
		return errors.New("model is required")
	}

	if len(r.RawInputItems) > 0 && r.RawAPIFormat != APIFormatOpenAIResponse {
		return errors.New("raw_input_items require OpenAI Responses api format")
	}
	rawInputItems, rawInputItemsOK := parseRawJSONArray(r.RawInputItems)
	if len(r.RawInputItems) > 0 && !rawInputItemsOK {
		return errors.New("raw_input_items must be a valid JSON array")
	}

	if r.PreviousResponseID != nil && strings.TrimSpace(*r.PreviousResponseID) != "" && r.RawAPIFormat != APIFormatOpenAIResponse {
		return errors.New("previous_response_id requires OpenAI Responses api format")
	}

	if r.IsOpenAIExactReplayRequest() {
		if r.PreviousResponseID != nil && strings.TrimSpace(*r.PreviousResponseID) != "" {
			return errors.New("replay_exact request must not include previous_response_id")
		}
		if len(r.RawInputItems) == 0 || rawInputItems == nil || len(rawInputItems) == 0 {
			return errors.New("replay_exact request requires raw_input_items")
		}
	}

	switch r.ResolveRequestType() {
	case RequestTypeEmbedding:
		if len(r.Messages) > 0 || len(r.RawInputItems) > 0 {
			return errors.New("cannot specify both messages and input")
		}
		if r.EmbeddingInput == nil {
			return errors.New("input is required")
		}
		if r.EmbeddingInput.Single == nil && len(r.EmbeddingInput.Multiple) == 0 {
			return errors.New("input cannot be empty")
		}
	case RequestTypeChat, RequestTypeResponses:
		if r.EmbeddingInput != nil {
			return errors.New("cannot specify both messages and input")
		}
		if len(r.Messages) == 0 && len(r.RawInputItems) == 0 {
			return errors.New("messages are required")
		}
		if len(r.Messages) > 0 {
			r.fillMissingToolCallIDsFromToolMessages()
			r.fillMissingToolCallIDs()
		}
	case RequestTypeImages, RequestTypeRerank:
		// The operation-specific payload was validated by normalizeRequestType.
	default:
		return errors.New("either messages or input is required")
	}

	return nil
}

func parseRawJSONArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}
	return items, true
}

func (r *InternalLLMRequest) fillMissingToolCallIDs() {
	usedIDs := make(map[string]struct{})
	for _, msg := range r.Messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID == "" {
				continue
			}
			usedIDs[tc.ID] = struct{}{}
		}
	}

	for messageIndex := range r.Messages {
		for toolCallIndex := range r.Messages[messageIndex].ToolCalls {
			toolCall := &r.Messages[messageIndex].ToolCalls[toolCallIndex]
			if toolCall.ID != "" {
				continue
			}

			base := stableToolCallID(*toolCall)
			candidate := base
			for sequence := 1; ; sequence++ {
				if _, exists := usedIDs[candidate]; !exists {
					break
				}
				candidate = fmt.Sprintf("%s_%d", base, sequence)
			}

			toolCall.ID = candidate
			usedIDs[candidate] = struct{}{}
		}
	}
}

func stableToolCallID(toolCall ToolCall) string {
	payload := struct {
		Type      string `json:"type,omitempty"`
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	}{
		Type:      toolCall.Type,
		Name:      toolCall.Function.Name,
		Arguments: canonicalJSONText(toolCall.Function.Arguments),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(toolCall.Type + "\x00" + toolCall.Function.Name + "\x00" + toolCall.Function.Arguments)
	}
	sum := sha256.Sum256(data)
	return "call_octopus_" + hex.EncodeToString(sum[:8])
}

func canonicalJSONText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return trimmed
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return trimmed
	}
	return string(encoded)
}

func (r *InternalLLMRequest) fillMissingToolCallIDsFromToolMessages() {
	for msgIndex := 0; msgIndex < len(r.Messages); msgIndex++ {
		msg := &r.Messages[msgIndex]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		candidates := make([]string, 0, len(msg.ToolCalls))
		for nextIndex := msgIndex + 1; nextIndex < len(r.Messages); nextIndex++ {
			nextMsg := r.Messages[nextIndex]
			if nextMsg.Role != "tool" {
				break
			}
			if nextMsg.ToolCallID == nil || *nextMsg.ToolCallID == "" {
				continue
			}
			candidates = append(candidates, *nextMsg.ToolCallID)
		}

		if len(candidates) == 0 {
			continue
		}

		used := make(map[string]struct{})
		for _, toolCall := range msg.ToolCalls {
			if toolCall.ID == "" {
				continue
			}
			used[toolCall.ID] = struct{}{}
		}

		candidateIndex := 0
		for toolCallIndex := range msg.ToolCalls {
			if msg.ToolCalls[toolCallIndex].ID != "" {
				continue
			}

			for candidateIndex < len(candidates) {
				candidate := candidates[candidateIndex]
				candidateIndex++
				if _, exists := used[candidate]; exists {
					continue
				}
				msg.ToolCalls[toolCallIndex].ID = candidate
				used[candidate] = struct{}{}
				break
			}
		}
	}
}

// IsEmbeddingRequest returns true if this is an embedding request.
func (r *InternalLLMRequest) IsEmbeddingRequest() bool {
	return r.ResolveRequestType() == RequestTypeEmbedding
}

// IsChatRequest returns true if this is a chat completion request.
func (r *InternalLLMRequest) IsChatRequest() bool {
	requestType := r.ResolveRequestType()
	return requestType == RequestTypeChat || requestType == RequestTypeResponses
}

func (r *InternalLLMRequest) MarkOpenAIResponsesPassthroughRequired(reason string) {
	if r == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	existing := r.OpenAIResponsesPassthroughReasonTextValue()
	if reason != "" && existing != "" {
		reason = existing + "," + reason
	} else if reason == "" {
		reason = existing
	}
	r.SetOpenAIExtensions(OpenAIExtension{
		ResponsesPassthroughRequired: true,
		ResponsesPassthroughReason:   reason,
	})
}

func (r *InternalLLMRequest) IsOpenAIExactReplayRequest() bool {
	return r.TransformerMetadataValue(TransformerMetadataWSExecutionMode) == TransformerMetadataWSExecutionModeReplayExact
}

func (r *InternalLLMRequest) MarkOpenAIExactReplayRequest() {
	if r == nil {
		return
	}
	r.SetTransformerMetadataValue(TransformerMetadataWSExecutionMode, TransformerMetadataWSExecutionModeReplayExact)
}

func (r *InternalLLMRequest) SetTransformerMetadataValue(key, value string) {
	if r == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if r.TransformerMetadata == nil {
		r.TransformerMetadata = map[string]string{}
	}
	r.TransformerMetadata[key] = strings.TrimSpace(value)
}

func (r *InternalLLMRequest) TransformerMetadataValue(key string) string {
	if r == nil || r.TransformerMetadata == nil {
		return ""
	}
	return strings.TrimSpace(r.TransformerMetadata[strings.TrimSpace(key)])
}

func (r *InternalLLMRequest) TransformerMetadataBool(key string) bool {
	return strings.EqualFold(r.TransformerMetadataValue(key), "true")
}

func (r *InternalLLMRequest) ClearHelpFields() {
	for i, msg := range r.Messages {
		msg.ClearHelpFields()
		r.Messages[i] = msg
	}

	r.ExtraBody = nil
	r.Include = nil
}

// NormalizeMessages applies Message.Normalize to every message in the
// request. Outbound transformers call this at the top of TransformRequest so
// that subsequent conversion code can assume messages carry valid, non-empty
// payloads.
func (r *InternalLLMRequest) NormalizeMessages() {
	for i := range r.Messages {
		r.Messages[i].Normalize()
	}
}

// EnforceMessageAlternation rewrites r.Messages so consecutive same-role
// turns are merged and provider-specific opening requirements are met.
// Intended to be called by outbound transformers whose upstream enforces
// strict user/assistant alternation (Anthropic and Gemini). Callers for
// lax providers (OpenAI) can safely skip this.
func (r *InternalLLMRequest) EnforceMessageAlternation(provider AlternationProvider) {
	r.Messages = EnforceAlternation(r.Messages, provider)
}

func (r *InternalLLMRequest) IsImageGenerationRequest() bool {
	return r.ResolveRequestType() == RequestTypeImages || len(r.Modalities) > 0 && slices.Contains(r.Modalities, "image")
}

type TransformOptions struct {
	// ArrayInputs specifies whether the original input was an array.
	ArrayInputs *bool `json:"-"`
}

type StreamOptions struct {
	// If set, an additional chunk will be streamed before the data: [DONE] message.
	// The usage field on this chunk shows the token usage statistics for the entire request,
	// and the choices field will always be an empty array.
	// All other chunks will also include a usage field, but with a null value.
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Stop struct {
	Stop         *string
	MultipleStop []string
}

func (s Stop) MarshalJSON() ([]byte, error) {
	if s.Stop != nil {
		return json.Marshal(s.Stop)
	}

	if len(s.MultipleStop) > 0 {
		return json.Marshal(s.MultipleStop)
	}

	return []byte("[]"), nil
}

func (s *Stop) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		s.Stop = &str
		return nil
	}

	var strs []string

	err = json.Unmarshal(data, &strs)
	if err == nil {
		s.MultipleStop = strs
		return nil
	}

	return errors.New("invalid stop type")
}

// ThinkingConfig represents the thinking mode configuration for DeepSeek models.
// Ref: https://api-docs.deepseek.com/guides/thinking_mode
type ThinkingConfig struct {
	// Type controls whether thinking mode is enabled. Valid values: "enabled" or "disabled".
	Type string `json:"type,omitempty"`
}
