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

func (o *MessageOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	if len(eventData) == 0 {
		return nil, nil
	}

	// Handle [DONE] marker
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &model.InternalLLMResponse{
			Object: "[DONE]",
		}, nil
	}

	// Initialize state if needed
	if !o.initialized {
		o.toolCalls = make(map[int]*model.ToolCall)
		o.toolIndex = -1
		o.initialized = true
	}

	// Parse the streaming event
	var streamEvent anthropicModel.StreamEvent
	if err := json.Unmarshal(eventData, &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	resp := &model.InternalLLMResponse{
		ID:      o.streamID,
		Model:   o.streamModel,
		Object:  "chat.completion.chunk",
		Created: 0,
	}

	switch streamEvent.Type {
	case "message_start":
		if streamEvent.Message != nil {
			o.streamID = streamEvent.Message.ID
			o.streamModel = streamEvent.Message.Model
			resp.ID = o.streamID
			resp.Model = o.streamModel

			// 上游只要返回了 usage 对象就采纳（即便全为 0），避免后续 input 计为 0。
			if streamEvent.Message.Usage != nil {
				o.streamUsage = convertAnthropicUsage(streamEvent.Message.Usage)
				resp.Usage = o.streamUsage
			}
		}

		resp.Choices = []model.Choice{
			{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
				},
			},
		}

	case "content_block_start":
		if streamEvent.ContentBlock != nil {
			switch streamEvent.ContentBlock.Type {
			case "tool_use":
				o.toolIndex++
				toolCall := model.ToolCall{
					Index: o.toolIndex,
					ID:    streamEvent.ContentBlock.ID,
					Type:  "function",
					Function: model.FunctionCall{
						Name:      lo.FromPtr(streamEvent.ContentBlock.Name),
						Arguments: "",
					},
				}
				o.toolCalls[o.toolIndex] = &toolCall

				resp.Choices = []model.Choice{
					{
						Index: 0,
						Delta: &model.Message{
							Role:      "assistant",
							ToolCalls: []model.ToolCall{toolCall},
						},
					},
				}
			case "text", "thinking":
				// These are handled in content_block_delta
				return nil, nil
			case "redacted_thinking":
				// Pass through as a complete block (no delta)
				resp.Choices = []model.Choice{
					{
						Index: 0,
						Delta: &model.Message{
							Role:                   "assistant",
							RedactedThinkingBlocks: []string{streamEvent.ContentBlock.Data},
							ReasoningBlocks: []model.ReasoningBlock{{
								Kind:     model.ReasoningBlockKindRedacted,
								Index:    -1,
								Data:     streamEvent.ContentBlock.Data,
								Provider: "anthropic",
							}},
						},
					},
				}
			default:
				return nil, nil
			}
		}

	case "content_block_delta":
		if streamEvent.Delta != nil && streamEvent.Delta.Type != nil {
			choice := model.Choice{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
				},
			}

			switch *streamEvent.Delta.Type {
			case "text_delta":
				if streamEvent.Delta.Text != nil {
					choice.Delta.Content = model.MessageContent{
						Content: streamEvent.Delta.Text,
					}
				}
			case "input_json_delta":
				if streamEvent.Delta.PartialJSON != nil && o.toolIndex >= 0 {
					choice.Delta.ToolCalls = []model.ToolCall{
						{
							Index: o.toolIndex,
							ID:    o.toolCalls[o.toolIndex].ID,
							Type:  "function",
							Function: model.FunctionCall{
								Arguments: *streamEvent.Delta.PartialJSON,
							},
						},
					}
				}
			case "thinking_delta":
				if streamEvent.Delta.Thinking != nil {
					choice.Delta.ReasoningContent = streamEvent.Delta.Thinking
				}
			case "signature_delta":
				if streamEvent.Delta.Signature != nil {
					signature := model.OpaqueSignature{
						Provider: model.SignatureProviderAnthropic,
						Kind:     model.OpaqueSignatureKindAnthropicThinking,
						Value:    *streamEvent.Delta.Signature,
					}
					choice.Delta.SetOpaqueReasoningSignature(signature)
					// Emit a standalone signature block so downstream aggregators can attach it
					// to the correct thinking block even when multiple thinking blocks exist.
					block := model.ReasoningBlock{Kind: model.ReasoningBlockKindSignature, Index: -1}
					block.SetOpaqueSignature(signature)
					choice.Delta.ReasoningBlocks = []model.ReasoningBlock{block}
				}
			default:
				return nil, nil
			}

			resp.Choices = []model.Choice{choice}
		}

	case "message_delta":
		if streamEvent.Usage != nil {
			usage := convertAnthropicUsage(streamEvent.Usage)
			if o.streamUsage != nil {
				// message_delta.usage normally carries only the final output_tokens.
				// Carry forward cache metadata captured at message_start so the
				// aggregate reflects all four buckets (input / output / cache_read /
				// cache_write) rather than collapsing to input+output.
				// 仅当 delta 未自带 input 时才继承，避免真实 input 被零值覆盖。
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
		}

		if streamEvent.Delta != nil && streamEvent.Delta.StopReason != nil {
			finishReason := convertStopReason(streamEvent.Delta.StopReason)
			resp.Choices = []model.Choice{
				{
					Index:        0,
					FinishReason: finishReason,
					StopSequence: streamEvent.Delta.StopSequence,
				},
			}
		}

	case "message_stop":
		resp.Choices = []model.Choice{}
		if o.streamUsage != nil {
			resp.Usage = o.streamUsage
		}

	case "content_block_stop", "ping":
		return nil, nil

	case "error":
		if streamEvent.Error == nil {
			return nil, nil
		}
		resp.Error = &model.ResponseError{
			StatusCode: mapAnthropicErrorTypeToStatus(streamEvent.Error.Type),
			Detail: model.ErrorDetail{
				Type:    streamEvent.Error.Type,
				Message: streamEvent.Error.Message,
			},
		}
		resp.Choices = nil

	default:
		return nil, nil
	}

	return resp, nil
}
