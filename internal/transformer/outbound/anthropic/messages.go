package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/httpio"
	"github.com/bestruirui/octopus/internal/transformer/model"
	anthropicModel "github.com/bestruirui/octopus/internal/transformer/protocol/anthropic"
	"github.com/bestruirui/octopus/internal/utils/log"
)

type MessageOutbound struct {
	// Stream state tracking
	streamID    string
	streamModel string
	streamUsage *model.Usage
	toolIndex   int
	toolCalls   map[int]*model.ToolCall
	initialized bool
}

func (o *MessageOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	request, anthropicReq, _ := prepareAnthropicRequest(request, request.Model)

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	// For streaming requests, Anthropic returns Server-Sent Events.
	if request.Stream != nil && *request.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("X-API-Key", key)
	if betas := collectAnthropicBetaHeaders(anthropicReq, request); len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}

	// Parse and set URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}

	parsedUrl.Path = parsedUrl.Path + "/messages"
	// Pass through the original query parameters exactly as-is
	if request.Query != nil {
		parsedUrl.RawQuery = request.Query.Encode()
	}
	req.URL = parsedUrl

	return req, nil
}

func (o *MessageOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("response is nil")
	}

	body, err := httpio.ReadResponseBody(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	// Check for error response
	if response.StatusCode >= 400 {
		var errResp anthropicModel.AnthropicError
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			if strings.Contains(strings.ToLower(errResp.Error.Message), "signature") {
				log.Warnw("transformer.reasoning.signature.passthrough",
					"provider", "anthropic",
					"direction", "error",
					"status_code", response.StatusCode,
					"error_type", errResp.Error.Type,
					"error_message", truncateForAudit(errResp.Error.Message, 256),
				)
			}
			return nil, &model.ResponseError{
				StatusCode: response.StatusCode,
				Detail: model.ErrorDetail{
					Message: errResp.Error.Message,
					Type:    errResp.Error.Type,
				},
			}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", response.StatusCode, string(body))
	}

	var anthropicResp anthropicModel.Message
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal anthropic response: %w", err)
	}

	// Convert to internal response
	return convertToLLMResponse(&anthropicResp), nil
}
