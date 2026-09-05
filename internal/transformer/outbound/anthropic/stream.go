package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/bestruirui/octopus/internal/transformer/model"
	anthropicModel "github.com/bestruirui/octopus/internal/transformer/protocol/anthropic"
	"github.com/samber/lo"
)

func (o *MessageOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	if len(eventData) == 0 {
		return nil, nil
	}
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return []model.StreamEvent{{Kind: model.StreamEventKindDone}}, nil
	}
	if !o.initialized {
		o.toolCalls = make(map[int]*model.ToolCall)
		o.toolIndex = -1
		o.initialized = true
	}

	var streamEvent anthropicModel.StreamEvent
	if err := json.Unmarshal(eventData, &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	events := make([]model.StreamEvent, 0, 2)
	appendUsage := func(usage *model.Usage) {
		if usage != nil {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindUsageDelta, ID: o.streamID, Model: o.streamModel, Usage: usage})
		}
	}

	switch streamEvent.Type {
	case "message_start":
		if streamEvent.Message != nil {
			o.streamID = streamEvent.Message.ID
			o.streamModel = streamEvent.Message.Model
			// 上游只要返回了 usage 对象就采纳（即便全为 0），以便 message_delta
			// 能继承 PromptTokens。部分第三方兼容商在 message_start 返回全零 usage，
			// 之前的 >0 过滤会整体丢弃，导致后续 input 计为 0。
			if streamEvent.Message.Usage != nil {
				o.streamUsage = convertAnthropicUsage(streamEvent.Message.Usage)
			}
		}
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStart, ID: o.streamID, Model: o.streamModel, Role: "assistant"})
		appendUsage(o.streamUsage)

	case "content_block_start":
		if streamEvent.ContentBlock == nil {
			return nil, nil
		}
		switch streamEvent.ContentBlock.Type {
		case "tool_use":
			o.toolIndex++
			toolCall := model.ToolCall{
				Index: o.toolIndex,
				ID:    streamEvent.ContentBlock.ID,
				Type:  "function",
				Function: model.FunctionCall{
					Name: lo.FromPtr(streamEvent.ContentBlock.Name),
				},
			}
			o.toolCalls[o.toolIndex] = &toolCall
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallStart, ID: o.streamID, Model: o.streamModel, Index: 0, ToolCall: &toolCall})
		case "text", "thinking":
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindContentBlockStart, ID: o.streamID, Model: o.streamModel, Index: 0, ContentBlock: &model.StreamContentBlock{Type: streamEvent.ContentBlock.Type}})
		case "redacted_thinking":
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindContentBlockStart, ID: o.streamID, Model: o.streamModel, Index: 0, ContentBlock: &model.StreamContentBlock{Type: "redacted_thinking", Data: streamEvent.ContentBlock.Data}})
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindContentBlockStop, ID: o.streamID, Model: o.streamModel, Index: 0, ContentBlock: &model.StreamContentBlock{Type: "redacted_thinking"}})
		default:
			return nil, nil
		}

	case "content_block_delta":
		if streamEvent.Delta == nil || streamEvent.Delta.Type == nil {
			return nil, nil
		}
		switch *streamEvent.Delta.Type {
		case "text_delta":
			if streamEvent.Delta.Text != nil {
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindTextDelta, ID: o.streamID, Model: o.streamModel, Index: 0, Delta: &model.StreamDelta{Text: *streamEvent.Delta.Text}})
			}
		case "input_json_delta":
			if streamEvent.Delta.PartialJSON != nil && o.toolIndex >= 0 {
				toolCall := model.ToolCall{Index: o.toolIndex, Type: "function", Function: model.FunctionCall{Arguments: *streamEvent.Delta.PartialJSON}}
				if existing := o.toolCalls[o.toolIndex]; existing != nil {
					toolCall.ID = existing.ID
				}
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallDelta, ID: o.streamID, Model: o.streamModel, Index: 0, ToolCall: &toolCall, Delta: &model.StreamDelta{Arguments: *streamEvent.Delta.PartialJSON}})
			}
		case "thinking_delta":
			if streamEvent.Delta.Thinking != nil {
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindThinkingDelta, ID: o.streamID, Model: o.streamModel, Index: 0, Delta: &model.StreamDelta{Thinking: *streamEvent.Delta.Thinking}})
			}
		case "signature_delta":
			if streamEvent.Delta.Signature != nil {
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindSignatureDelta, ID: o.streamID, Model: o.streamModel, Index: 0, Delta: &model.StreamDelta{
					Signature: *streamEvent.Delta.Signature,
					SignatureSource: &model.OpaqueSignature{
						Provider: model.SignatureProviderAnthropic,
						Kind:     model.OpaqueSignatureKindAnthropicThinking,
						Value:    *streamEvent.Delta.Signature,
					},
				}})
			}
		default:
			return nil, nil
		}

	case "message_delta":
		if streamEvent.Usage != nil {
			usage := convertAnthropicUsage(streamEvent.Usage)
			if o.streamUsage != nil {
				// message_delta 自身通常只带 output；input 来自 message_start。
				// 仅当 delta 未携带 input 时才继承，避免上游把真实 input 放在
				// message_delta 时被 message_start 的零值覆盖。
				if usage.PromptTokens == 0 {
					usage.PromptTokens = o.streamUsage.PromptTokens
				}
				if usage.CacheCreationInputTokens == 0 {
					usage.CacheCreationInputTokens = o.streamUsage.CacheCreationInputTokens
				}
				if usage.CacheReadInputTokens == 0 {
					usage.CacheReadInputTokens = o.streamUsage.CacheReadInputTokens
				}
				if usage.CacheCreation5mInputTokens == 0 {
					usage.CacheCreation5mInputTokens = o.streamUsage.CacheCreation5mInputTokens
				}
				if usage.CacheCreation1hInputTokens == 0 {
					usage.CacheCreation1hInputTokens = o.streamUsage.CacheCreation1hInputTokens
				}
				if usage.PromptTokensDetails == nil {
					usage.PromptTokensDetails = o.streamUsage.PromptTokensDetails
				}
			}
			usage.TotalTokens = usage.EffectiveInputTokens() + usage.CompletionTokens
			o.streamUsage = usage
			appendUsage(usage)
		}
		if streamEvent.Delta != nil && streamEvent.Delta.StopReason != nil {
			finishReason := convertStopReason(streamEvent.Delta.StopReason)
			if finishReason != nil {
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: o.streamID, Model: o.streamModel, StopReason: model.ParseFinishReason(*finishReason), StopSequence: streamEvent.Delta.StopSequence})
			}
		}

	case "message_stop":
		appendUsage(o.streamUsage)

	case "content_block_stop":
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindContentBlockStop, ID: o.streamID, Model: o.streamModel, Index: 0})

	case "ping":
		return nil, nil

	case "error":
		if streamEvent.Error == nil {
			return nil, nil
		}
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindError, ID: o.streamID, Model: o.streamModel, Error: &model.ResponseError{StatusCode: mapAnthropicErrorTypeToStatus(streamEvent.Error.Type), Detail: model.ErrorDetail{Type: streamEvent.Error.Type, Message: streamEvent.Error.Message}}})

	default:
		return nil, nil
	}

	if len(events) == 0 {
		return nil, nil
	}
	return events, nil
}
