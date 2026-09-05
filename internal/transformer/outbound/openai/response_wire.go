package openai

import (
	"encoding/json"
	"fmt"
)

// ResponsesRequest represents the OpenAI Responses API request format.
type ResponsesRequest struct {
	Model             string                `json:"model"`
	Instructions      string                `json:"instructions,omitempty"`
	Input             ResponsesInput        `json:"input"`
	Tools             []ResponsesTool       `json:"tools,omitempty"`
	ToolChoice        *ResponsesToolChoice  `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool                 `json:"parallel_tool_calls,omitempty"`
	Stream            *bool                 `json:"stream,omitempty"`
	Text              *ResponsesTextOptions `json:"text,omitempty"`
	Store             *bool                 `json:"store,omitempty"`
	ServiceTier       *string               `json:"service_tier,omitempty"`
	Truncation        *string               `json:"truncation,omitempty"`
	User              *string               `json:"user,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
	MaxOutputTokens   *int64                `json:"max_output_tokens,omitempty"`
	Temperature       *float64              `json:"temperature,omitempty"`
	TopP              *float64              `json:"top_p,omitempty"`
	Reasoning         *ResponsesReasoning   `json:"reasoning,omitempty"`

	// Pass-through fields
	PreviousResponseID   *string         `json:"previous_response_id,omitempty"`
	Background           *bool           `json:"background,omitempty"`
	Prompt               json.RawMessage `json:"prompt,omitempty"`
	PromptCacheKey       *string         `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention *string         `json:"prompt_cache_retention,omitempty"`
	SafetyIdentifier     *string         `json:"safety_identifier,omitempty"`
	MaxToolCalls         *int64          `json:"max_tool_calls,omitempty"`
	Conversation         json.RawMessage `json:"conversation,omitempty"`
	ContextManagement    json.RawMessage `json:"context_management,omitempty"`
	StreamOptions        json.RawMessage `json:"stream_options,omitempty"`
	Include              []string        `json:"include,omitempty"`
	TopLogprobs          *int64          `json:"top_logprobs,omitempty"`
}

type ResponsesInput struct {
	Text  *string
	Items []ResponsesItem
	Raw   json.RawMessage
}

func (i ResponsesInput) MarshalJSON() ([]byte, error) {
	if len(i.Raw) > 0 {
		return json.Marshal(json.RawMessage(i.Raw))
	}
	if i.Text != nil {
		return json.Marshal(i.Text)
	}
	return json.Marshal(i.Items)
}

func (i *ResponsesInput) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		i.Text = &text
		return nil
	}
	var items []ResponsesItem
	if err := json.Unmarshal(data, &items); err == nil {
		i.Items = items
		return nil
	}
	return fmt.Errorf("invalid input format")
}

type ResponsesItem struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Role     string          `json:"role,omitempty"`
	Content  *ResponsesInput `json:"content,omitempty"`
	Status   *string         `json:"status,omitempty"`
	Text     *string         `json:"text,omitempty"`
	Refusal  *string         `json:"refusal,omitempty"`
	ImageURL *string         `json:"image_url,omitempty"`
	Detail   *string         `json:"detail,omitempty"`

	// Annotations for output_text content
	Annotations []ResponsesAnnotation `json:"annotations,omitempty"`

	// Function call fields
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// Function call output
	Output        *ResponsesInput `json:"output,omitempty"`
	ItemReference *string         `json:"item_reference,omitempty"`

	// Image generation fields
	Result       *string `json:"result,omitempty"`
	Background   *string `json:"background,omitempty"`
	OutputFormat *string `json:"output_format,omitempty"`
	Quality      *string `json:"quality,omitempty"`
	Size         *string `json:"size,omitempty"`

	// Reasoning fields
	Summary          []ResponsesReasoningSummary `json:"summary,omitempty"`
	EncryptedContent *string                     `json:"encrypted_content,omitempty"`

	// Multimodal input passthrough for Responses→Responses routing. O-H6.
	FileID     *string              `json:"file_id,omitempty"`
	Filename   *string              `json:"filename,omitempty"`
	FileData   *string              `json:"file_data,omitempty"`
	FileURL    *string              `json:"file_url,omitempty"`
	InputAudio *ResponsesInputAudio `json:"input_audio,omitempty"`
}

// ResponsesInputAudio mirrors OpenAI's nested `input_audio` object for audio
// content parts on Responses input. O-H6.
type ResponsesInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format,omitempty"`
}

type ResponsesReasoningSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponsesAnnotation struct {
	Type       string  `json:"type"`
	StartIndex *int    `json:"start_index,omitempty"`
	EndIndex   *int    `json:"end_index,omitempty"`
	URL        *string `json:"url,omitempty"`
	Title      *string `json:"title,omitempty"`
	FileID     *string `json:"file_id,omitempty"`
	Filename   *string `json:"filename,omitempty"`
}

type ResponsesTool struct {
	Type              string          `json:"type,omitempty"`
	Name              string          `json:"name,omitempty"`
	Description       string          `json:"description,omitempty"`
	Parameters        map[string]any  `json:"parameters,omitempty"`
	Strict            *bool           `json:"strict,omitempty"`
	Background        string          `json:"background,omitempty"`
	OutputFormat      string          `json:"output_format,omitempty"`
	Quality           string          `json:"quality,omitempty"`
	Size              string          `json:"size,omitempty"`
	OutputCompression *int64          `json:"output_compression,omitempty"`
	Raw               json.RawMessage `json:"-"`
}

func (t ResponsesTool) MarshalJSON() ([]byte, error) {
	if len(t.Raw) > 0 {
		return json.Marshal(json.RawMessage(t.Raw))
	}
	type alias ResponsesTool
	return json.Marshal(alias(t))
}

func responsesToolsFromRaw(raw json.RawMessage) ([]ResponsesTool, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var rawTools []json.RawMessage
	if err := json.Unmarshal(raw, &rawTools); err != nil || rawTools == nil {
		return nil, false
	}
	tools := make([]ResponsesTool, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tools = append(tools, ResponsesTool{Raw: cloneRawJSON(rawTool)})
	}
	return tools, true
}

type ResponsesToolChoice struct {
	Mode *string `json:"mode,omitempty"`
	Type *string `json:"type,omitempty"`
	Name *string `json:"name,omitempty"`
}

func (t ResponsesToolChoice) MarshalJSON() ([]byte, error) {
	// If only Mode is set and it's a simple mode like "auto", "none", "required"
	if t.Mode != nil && t.Type == nil && t.Name == nil {
		return json.Marshal(*t.Mode)
	}
	// Otherwise, serialize as an object
	type Alias ResponsesToolChoice
	return json.Marshal(Alias(t))
}

type ResponsesTextOptions struct {
	Format    *ResponsesTextFormat `json:"format,omitempty"`
	Verbosity *string              `json:"verbosity,omitempty"`
}

type ResponsesTextFormat struct {
	Type   string          `json:"type,omitempty"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

type ResponsesReasoning struct {
	Effort          string  `json:"effort,omitempty"`
	MaxTokens       *int64  `json:"max_tokens,omitempty"`
	Summary         *string `json:"summary,omitempty"`
	GenerateSummary *string `json:"generate_summary,omitempty"`
}

// ResponsesResponse represents the OpenAI Responses API response format.
type ResponsesResponse struct {
	Object    string          `json:"object"`
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	CreatedAt int64           `json:"created_at"`
	Output    []ResponsesItem `json:"output"`
	Status    *string         `json:"status,omitempty"`
	Usage     *ResponsesUsage `json:"usage,omitempty"`
	Error     *ResponsesError `json:"error,omitempty"`
}

type ResponsesUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	InputTokenDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens       int64 `json:"output_tokens"`
	OutputTokenDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	TotalTokens int64 `json:"total_tokens"`
}

type ResponsesError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ResponsesStreamEvent struct {
	Type           string             `json:"type"`
	SequenceNumber int                `json:"sequence_number"`
	Response       *ResponsesResponse `json:"response,omitempty"`
	OutputIndex    int                `json:"output_index"`
	Item           *ResponsesItem     `json:"item,omitempty"`
	ItemID         *string            `json:"item_id,omitempty"`
	ContentIndex   *int               `json:"content_index,omitempty"`
	Delta          string             `json:"delta,omitempty"`
	Text           string             `json:"text,omitempty"`
	Name           string             `json:"name,omitempty"`
	Namespace      string             `json:"namespace,omitempty"`
	CallID         string             `json:"call_id,omitempty"`
	Arguments      string             `json:"arguments,omitempty"`
	SummaryIndex   *int               `json:"summary_index,omitempty"`
	Code           string             `json:"code,omitempty"`
	Message        string             `json:"message,omitempty"`
}
