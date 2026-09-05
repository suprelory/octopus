package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/compat"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

func (i *MessagesInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
	// Handle upstream error event: forward as Anthropic SSE `event: error` and
	// terminate the stream. Reference:
	// https://docs.anthropic.com/en/api/messages-streaming#error-events
	if stream != nil && stream.Error != nil {
		errType := stream.Error.Detail.Type
		if errType == "" {
			errType = "api_error"
		}
		errPayload := StreamEvent{
			Type: "error",
			Error: &ErrorDetail{
				Type:    errType,
				Message: stream.Error.Detail.Message,
			},
		}
		data, err := json.Marshal(errPayload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal error event: %w", err)
		}
		i.messageStopped = true
		return formatSSEEvent("error", data), nil
	}

	// Handle [DONE] marker
	if stream.Object == "[DONE]" {
		if i.hasStarted && !i.messageStopped {
			events, err := i.finalizeStreamAtEnd(i.pendingUsage)
			if err != nil {
				return nil, err
			}
			if len(events) == 0 {
				return nil, nil
			}
			return joinSSEEvents(events), nil
		}
		return nil, nil
	}

	// Store the chunk for aggregation
	i.streamAggregator.Add(stream)
	if stream.Usage != nil {
		i.pendingUsage = stream.Usage
	}

	var events [][]byte

	// Initialize message ID and model from first chunk
	if i.messageID == "" && stream.ID != "" {
		i.messageID = stream.ID
	}
	if i.modelName == "" && stream.Model != "" {
		i.modelName = stream.Model
	}

	// Generate message_start event if this is the first chunk
	if !i.hasStarted {
		i.hasStarted = true

		usage := &Usage{
			InputTokens:  i.inputToken,
			OutputTokens: 1,
		}
		if stream.Usage != nil {
			usage = i.convertUsage(stream.Usage)
		}

		startEvent := StreamEvent{
			Type: "message_start",
			Message: &StreamMessage{
				ID:      i.messageID,
				Type:    "message",
				Role:    "assistant",
				Model:   i.modelName,
				Content: []MessageContentBlock{},
				Usage:   usage,
			},
		}

		data, err := json.Marshal(startEvent)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal message_start event: %w", err)
		}
		events = append(events, formatSSEEvent("message_start", data))
	}

	// Process the current chunk
	if len(stream.Choices) > 0 {
		choice := stream.Choices[0]
		wireReasoningSignature := ""

		if choice.Delta != nil && len(choice.Delta.ReasoningBlocks) > 0 {
			for _, rb := range choice.Delta.ReasoningBlocks {
				switch rb.Kind {
				case model.ReasoningBlockKindThinking:
					if rb.Text != "" {
						choice.Delta.ReasoningContent = &rb.Text
					}
					if signature := reasoningBlockSignatureForProvider(rb, model.SignatureProviderAnthropic, model.OpaqueSignatureKindAnthropicThinking); signature != "" {
						wireReasoningSignature = signature
					}
				case model.ReasoningBlockKindSignature:
					if signature := reasoningBlockSignatureForProvider(rb, model.SignatureProviderAnthropic, model.OpaqueSignatureKindAnthropicThinking); signature != "" {
						wireReasoningSignature = signature
					} else if signature := geminiToolCallShimSignature(rb); signature != "" {
						wireReasoningSignature = signature
					}
				case model.ReasoningBlockKindRedacted:
					if rb.Data != "" {
						choice.Delta.RedactedThinkingBlocks = append(choice.Delta.RedactedThinkingBlocks, rb.Data)
					}
				}
			}
		}
		if wireReasoningSignature == "" {
			wireReasoningSignature = messageReasoningSignatureForProvider(choice.Delta, model.SignatureProviderAnthropic, model.OpaqueSignatureKindAnthropicThinking)
		}

		// Handle reasoning content (thinking) delta
		if choice.Delta != nil && choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			// If the tool content has started before the thinking content, we need to stop it
			if i.hasToolContentStarted {
				i.hasToolContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// Generate content_block_start if this is the first thinking content
			if !i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = true

				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type:      "thinking",
						Thinking:  lo.ToPtr(""),
						Signature: lo.ToPtr(""),
					},
				}
				data, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", data))
			}

			// Generate content_block_delta for thinking
			deltaEvent := StreamEvent{
				Type:  "content_block_delta",
				Index: &i.contentIndex,
				Delta: &StreamDelta{
					Type:     lo.ToPtr("thinking_delta"),
					Thinking: choice.Delta.ReasoningContent,
				},
			}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			events = append(events, formatSSEEvent("content_block_delta", data))
		}

		// Add signature delta if signature is available
		if wireReasoningSignature != "" {
			if !i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = true
				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type:      "thinking",
						Thinking:  lo.ToPtr(""),
						Signature: lo.ToPtr(""),
					},
				}
				data, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", data))
			}
			sigEvent := StreamEvent{
				Type:  "content_block_delta",
				Index: &i.contentIndex,
				Delta: &StreamDelta{
					Type:      lo.ToPtr("signature_delta"),
					Signature: &wireReasoningSignature,
				},
			}
			data, err := json.Marshal(sigEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal signature_delta event: %w", err)
			}
			events = append(events, formatSSEEvent("content_block_delta", data))
		}

		// Handle redacted thinking blocks (complete blocks, not deltas)
		if choice.Delta != nil && len(choice.Delta.RedactedThinkingBlocks) > 0 {
			// Close any open thinking content block first
			if i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = false
				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				stopData, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", stopData))
				i.contentIndex++
			}

			for _, rtData := range choice.Delta.RedactedThinkingBlocks {
				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type: "redacted_thinking",
						Data: rtData,
					},
				}
				startData, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", startData))

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				stopData, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", stopData))
				i.contentIndex++
			}
		}

		// Handle content delta
		if choice.Delta != nil && choice.Delta.Content.Content != nil && *choice.Delta.Content.Content != "" {
			// If the thinking content has started before the text content, we need to stop it
			if i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// If the tool content has started before the content block, we need to stop it
			if i.hasToolContentStarted {
				i.hasToolContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// Generate content_block_start if this is the first content
			if !i.hasTextContentStarted {
				i.hasTextContentStarted = true

				startEvent := StreamEvent{
					Type:  "content_block_start",
					Index: &i.contentIndex,
					ContentBlock: &MessageContentBlock{
						Type: "text",
						Text: lo.ToPtr(""),
					},
				}
				data, err := json.Marshal(startEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_start", data))
			}

			// Generate content_block_delta
			deltaEvent := StreamEvent{
				Type:  "content_block_delta",
				Index: &i.contentIndex,
				Delta: &StreamDelta{
					Type: lo.ToPtr("text_delta"),
					Text: choice.Delta.Content.Content,
				},
			}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			events = append(events, formatSSEEvent("content_block_delta", data))
		}

		// Handle tool calls
		if choice.Delta != nil && len(choice.Delta.ToolCalls) > 0 {
			// If the thinking content has started before the tool content, we need to stop it
			if i.hasThinkingContentStarted {
				i.hasThinkingContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// If the text content has started before the tool content, we need to stop it
			if i.hasTextContentStarted {
				i.hasTextContentStarted = false

				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))

				i.contentIndex++
			}

			// Initialize tool call index tracking if needed
			if i.toolCallIndices == nil {
				i.toolCallIndices = make(map[int]bool)
			}

			for _, deltaToolCall := range choice.Delta.ToolCalls {
				toolCallIndex := deltaToolCall.Index

				// Initialize tool call if it doesn't exist
				if !i.toolCallIndices[toolCallIndex] || !i.hasToolContentStarted {
					// 只有当此前确实已经打开过一个 tool_use 块（即将开启第二个或之后的
					// 工具块）时才发 stop；用 toolCallIndex>0 判断不可靠，因为上游
					// （尤其是 Responses API）把 OutputIndex 写入该字段，首个工具块
					// 的 OutputIndex 往往已经大于 0（前面可能先出现 message/reasoning
					// item），此时若发出 content_block_stop 会引用一个从未打开的块，
					// 触发客户端 "Content block not found"。
					if i.hasToolContentStarted {
						stopEvent := StreamEvent{
							Type:  "content_block_stop",
							Index: &i.contentIndex,
						}
						data, err := json.Marshal(stopEvent)
						if err != nil {
							return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
						}
						events = append(events, formatSSEEvent("content_block_stop", data))

						i.contentIndex++
					}

					i.toolCallIndices[toolCallIndex] = true
					i.hasToolContentStarted = true

					startBlock := &MessageContentBlock{
						Type:  "tool_use",
						ID:    deltaToolCall.ID,
						Name:  &deltaToolCall.Function.Name,
						Input: json.RawMessage("{}"),
					}
					if sig := deltaToolCall.GetGeminiExtensions().ThoughtSignature; strings.TrimSpace(sig) != "" {
						compat.SaveGeminiThoughtSignatureScoped(i.geminiSignatureScope(ctx, i.modelName), deltaToolCall.ID, deltaToolCall.Function.Name, sig)
					}
					startEvent := StreamEvent{
						Type:         "content_block_start",
						Index:        &i.contentIndex,
						ContentBlock: startBlock,
					}
					data, err := json.Marshal(startEvent)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
					}
					events = append(events, formatSSEEvent("content_block_start", data))

					// If the tool call has arguments, we need to generate a content_block_delta
					if deltaToolCall.Function.Arguments != "" {
						deltaEvent := StreamEvent{
							Type:  "content_block_delta",
							Index: &i.contentIndex,
							Delta: &StreamDelta{
								Type:        lo.ToPtr("input_json_delta"),
								PartialJSON: &deltaToolCall.Function.Arguments,
							},
						}
						data, err := json.Marshal(deltaEvent)
						if err != nil {
							return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
						}
						events = append(events, formatSSEEvent("content_block_delta", data))
					}
				} else {
					// Generate content_block_delta for input_json_delta
					deltaEvent := StreamEvent{
						Type:  "content_block_delta",
						Index: &i.contentIndex,
						Delta: &StreamDelta{
							Type:        lo.ToPtr("input_json_delta"),
							PartialJSON: &deltaToolCall.Function.Arguments,
						},
					}
					data, err := json.Marshal(deltaEvent)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
					}
					events = append(events, formatSSEEvent("content_block_delta", data))
				}
			}
		}

		// Handle finish reason
		if choice.FinishReason != nil && !i.hasFinished {
			i.hasFinished = true

			if i.hasOpenContentBlock() {
				stopEvent := StreamEvent{
					Type:  "content_block_stop",
					Index: &i.contentIndex,
				}
				data, err := json.Marshal(stopEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
				}
				events = append(events, formatSSEEvent("content_block_stop", data))
				i.resetOpenContentState()
			}

			// Convert finish reason to Anthropic format
			stopReason := model.ParseFinishReason(*choice.FinishReason).ToAnthropic()
			if stopReason == "" {
				stopReason = "end_turn"
			}

			// Store the stop reason, but don't generate message_delta yet
			// We'll wait for the usage chunk to combine them
			i.stopReason = &stopReason
			if choice.StopSequence != nil {
				i.stopSequence = choice.StopSequence
			}
		}
	}

	// Handle usage chunk after finish_reason
	if stream.Usage != nil && i.hasFinished && !i.messageStopped {
		finalEvents, err := i.finalizeStreamMessage(stream.Usage)
		if err != nil {
			return nil, err
		}
		events = append(events, finalEvents...)
	}

	if len(events) == 0 {
		return nil, nil
	}

	return joinSSEEvents(events), nil
}
