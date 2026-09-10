package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func (o *MessagesOutbound) nextReasoningIndex(candidateIndex int) int {
	if o.streamReasoningIndex == nil {
		o.streamReasoningIndex = make(map[int]int)
	}
	idx := o.streamReasoningIndex[candidateIndex]
	o.streamReasoningIndex[candidateIndex] = idx + 1
	return idx
}

func (o *MessagesOutbound) nextToolCallIndex() int {
	idx := o.streamToolCallIndex
	o.streamToolCallIndex++
	return idx
}

// TransformSourceEvent converts a complete Gemini provider envelope. Gemini
// generally puts lifecycle data in JSON, but compatible SSE servers may expose
// a standalone done event through the envelope metadata.
func (o *MessagesOutbound) TransformSourceEvent(ctx context.Context, event model.SourceEvent) ([]model.StreamEvent, error) {
	eventData := event.Data
	eventType := strings.TrimSpace(event.Type)
	if bytes.Equal(bytes.TrimSpace(eventData), []byte("[DONE]")) || eventType == "[DONE]" || strings.EqualFold(eventType, "done") {
		if eventType == "" {
			eventType = "[DONE]"
		}
		return []model.StreamEvent{{Kind: model.StreamEventKindDone, Terminal: true, TerminalEvent: eventType}}, nil
	}
	if len(bytes.TrimSpace(eventData)) == 0 {
		return nil, nil
	}

	var geminiResp model.GeminiGenerateContentResponse
	if err := json.Unmarshal(eventData, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gemini stream chunk: %w", err)
	}

	events := make([]model.StreamEvent, 0, len(geminiResp.Candidates)*4+1)
	for _, candidate := range geminiResp.Candidates {
		if candidate == nil {
			continue
		}
		base := model.StreamEvent{ID: geminiResp.ResponseId, Model: geminiResp.ModelVersion, Index: candidate.Index}
		if candidate.Content != nil {
			role := candidate.Content.Role
			if role == "model" || role == "" {
				role = "assistant"
			}
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStart, ID: base.ID, Model: base.Model, Index: base.Index, Role: role})
			for _, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}
				if part.Thought {
					if part.Text != "" || part.ThoughtSignature != "" {
						o.nextReasoningIndex(candidate.Index)
						events = append(events, model.StreamEvent{Kind: model.StreamEventKindThinkingDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Thinking: part.Text, Signature: part.ThoughtSignature, SignatureSource: geminiOpaqueSignature(part.ThoughtSignature, "", ""), ProviderExtensions: geminiThoughtSignatureProviderExtension(part.ThoughtSignature)}})
					}
					continue
				}
				if part.Text != "" {
					events = append(events, model.StreamEvent{Kind: model.StreamEventKindTextDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Text: part.Text}})
					if part.ThoughtSignature != "" {
						o.nextReasoningIndex(candidate.Index)
						events = append(events, model.StreamEvent{Kind: model.StreamEventKindSignatureDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Signature: part.ThoughtSignature, SignatureSource: geminiOpaqueSignature(part.ThoughtSignature, "", ""), ProviderExtensions: geminiThoughtSignatureProviderExtension(part.ThoughtSignature)}})
					}
				}
				if part.FunctionCall != nil {
					toolIndex := o.nextToolCallIndex()
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					toolCall := model.ToolCall{
						Index: toolIndex,
						ID:    geminiFunctionCallID(part.FunctionCall, toolIndex, base.ID, part.ThoughtSignature),
						Type:  "function",
						Function: model.FunctionCall{
							Name: part.FunctionCall.Name,
						},
						ThoughtSignature:   part.ThoughtSignature,
						ProviderExtensions: geminiThoughtSignatureProviderExtension(part.ThoughtSignature),
					}
					if part.ThoughtSignature != "" {
						o.nextReasoningIndex(candidate.Index)
						events = append(events, model.StreamEvent{Kind: model.StreamEventKindSignatureDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Signature: part.ThoughtSignature, SignatureSource: geminiOpaqueSignature(part.ThoughtSignature, toolCall.ID, toolCall.Function.Name), ProviderExtensions: geminiThoughtSignatureProviderExtension(part.ThoughtSignature)}})
					}
					events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallStart, ID: base.ID, Model: base.Model, Index: base.Index, ToolCall: &toolCall})
					toolDelta := toolCall
					toolDelta.Function.Arguments = string(argsJSON)
					events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallDelta, ID: base.ID, Model: base.Model, Index: base.Index, ToolCall: &toolDelta, Delta: &model.StreamDelta{Arguments: string(argsJSON), ProviderExtensions: geminiThoughtSignatureProviderExtension(part.ThoughtSignature)}})
					events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallStop, ID: base.ID, Model: base.Model, Index: base.Index, ToolCall: &toolCall})
				}
			}
		}
		if candidate.FinishReason != nil {
			reason := convertGeminiFinishReason(*candidate.FinishReason)
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: base.ID, Model: base.Model, Index: base.Index, StopReason: model.ParseFinishReason(reason)})
		}
	}
	if usage := convertGeminiUsageMetadata(geminiResp.UsageMetadata); usage != nil {
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindUsageDelta, ID: geminiResp.ResponseId, Model: geminiResp.ModelVersion, Usage: usage})
	}
	if len(geminiResp.Candidates) == 0 && geminiResp.PromptFeedback != nil && geminiResp.PromptFeedback.BlockReason != "" {
		reason := model.FinishReasonFromGemini(geminiResp.PromptFeedback.BlockReason)
		if reason == "" {
			reason = model.FinishReasonContentFilter
		}
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStart, ID: geminiResp.ResponseId, Model: geminiResp.ModelVersion, Role: "assistant"})
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: geminiResp.ResponseId, Model: geminiResp.ModelVersion, StopReason: reason, Terminal: true, TerminalEvent: "prompt_feedback"})
	}
	return events, nil
}

// TransformStreamEvent retains the byte-only compatibility contract.
func (o *MessagesOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	return o.TransformSourceEvent(ctx, model.SourceEvent{Data: eventData})
}

func (o *MessagesOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	// Keep the historical aggregate path stable for callers that still consume
	// InternalLLMResponse directly. Relay uses TransformSourceEvent below the
	// canonical event boundary.
	if bytes.HasPrefix(eventData, []byte("[DONE]")) || len(eventData) == 0 {
		return &model.InternalLLMResponse{Object: "[DONE]"}, nil
	}
	var geminiResp model.GeminiGenerateContentResponse
	if err := json.Unmarshal(eventData, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gemini stream chunk: %w", err)
	}
	return convertGeminiToLLMResponse(&geminiResp, true, o.nextReasoningIndex), nil
}
