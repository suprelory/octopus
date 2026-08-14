package volcengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

var supportedReasoningEffortModel = map[string]bool{
	"doubao-seed-1-8-251228":      true,
	"doubao-seed-1-6-lite-251015": true,
	"doubao-seed-1-6-251015":      true,
}

type ResponseOutbound struct {
	inner openai.ResponseOutbound
}

func (o *ResponseOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	request = request.Clone()

	request.NormalizeMessages()

	// Convert to Responses API request format
	openaiReq := openai.ConvertToResponsesRequest(request)
	openaiReq.Metadata = nil // volcengine not supported
	if _, ok := supportedReasoningEffortModel[request.Model]; !ok {
		openaiReq.Reasoning = nil
	}
	responsesReq := ResponsesRequest{
		ResponsesRequest: openaiReq,
		Input:            convertToResponsesInput(openaiReq.Input),
	}
	switch request.ReasoningEffort {
	case "minimal":
		responsesReq.Thinking.Type = ThinkingTypeDisabled
	case "low", "medium", "high":
		responsesReq.Thinking.Type = ThinkingTypeEnabled
	default:
	}

	body, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal responses api request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	// Parse and set URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedUrl.Path = parsedUrl.Path + "/responses"
	req.URL = parsedUrl
	req.Method = http.MethodPost

	return req, nil

}
func (o *ResponseOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	return o.inner.TransformResponse(ctx, response)
}

func (o *ResponseOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	return o.inner.TransformStream(ctx, eventData)
}

func (o *ResponseOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	return o.inner.TransformStreamEvent(ctx, eventData)
}

type ResponsesRequest struct {
	*openai.ResponsesRequest
	Input    ResponsesInput `json:"input"`
	Thinking Thinking       `json:"thinking,omitzero"`
}

type ThinkingType string

const (
	ThinkingTypeAuto     ThinkingType = "auto"
	ThinkingTypeDisabled ThinkingType = "disabled"
	ThinkingTypeEnabled  ThinkingType = "enabled"
)

type Thinking struct {
	Type ThinkingType `json:"type"`
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
	openai.ResponsesItem
	Partial bool `json:"partial,omitempty"`
}

func convertToResponsesInput(input openai.ResponsesInput) ResponsesInput {
	if len(input.Raw) > 0 {
		return ResponsesInput{Raw: markLastAssistantPartial(input.Raw)}
	}
	result := ResponsesInput{Items: make([]ResponsesItem, 0, len(input.Items))}
	if input.Text != nil {
		result.Text = input.Text
		return result
	}

	for _, item := range input.Items {
		result.Items = append(result.Items, ResponsesItem{ResponsesItem: item})
	}
	if len(result.Items) == 0 {
		return result
	}
	// If the role of the last message is the assistant, needs set partial.
	idx := len(input.Items) - 1
	if result.Items[idx].Role == "assistant" {
		result.Items[idx].Partial = true
	}
	return result
}

func markLastAssistantPartial(raw json.RawMessage) json.RawMessage {
	result := append(json.RawMessage(nil), raw...)
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return result
	}
	last := items[len(items)-1]
	var role string
	if err := json.Unmarshal(last["role"], &role); err != nil || role != "assistant" {
		return result
	}
	last["partial"] = json.RawMessage("true")
	encoded, err := json.Marshal(items)
	if err != nil {
		return result
	}
	return encoded
}
