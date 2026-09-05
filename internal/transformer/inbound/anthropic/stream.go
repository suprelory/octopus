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

func (i *MessagesInbound) TransformStreamEvents(ctx context.Context, events []model.StreamEvent) ([]byte, error) {
	if len(events) == 0 {
		return nil, nil
	}
	if stream := model.InternalResponseFromStreamEvents(events); stream != nil && stream.Object != "[DONE]" {
		i.streamAggregator.Add(stream)
	}

	var firstUsage *model.Usage
	for _, event := range events {
		if event.Usage != nil {
			if firstUsage == nil {
				firstUsage = event.Usage
			}
			i.pendingUsage = event.Usage
		}
	}

	var out [][]byte
	ensureStarted := func(event model.StreamEvent) error {
		if event.ID != "" {
			i.messageID = event.ID
		}
		if event.Model != "" {
			i.modelName = event.Model
		}
		if i.hasStarted {
			return nil
		}
		i.hasStarted = true
		usage := &Usage{InputTokens: i.inputToken, OutputTokens: 1}
		if firstUsage != nil {
			usage = i.convertUsage(firstUsage)
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
			return fmt.Errorf("failed to marshal message_start event: %w", err)
		}
		out = append(out, formatSSEEvent("message_start", data))
		return nil
	}
	closeOpenBlock := func() error {
		if !i.hasOpenContentBlock() {
			return nil
		}
		stopEvent := StreamEvent{Type: "content_block_stop", Index: &i.contentIndex}
		data, err := json.Marshal(stopEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_stop event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_stop", data))
		i.resetOpenContentState()
		i.contentIndex++
		return nil
	}
	startText := func() error {
		if i.hasTextContentStarted {
			return nil
		}
		if err := closeOpenBlock(); err != nil {
			return err
		}
		i.hasTextContentStarted = true
		startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: &MessageContentBlock{Type: "text", Text: lo.ToPtr("")}}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_start event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_start", data))
		return nil
	}
	startThinking := func() error {
		if i.hasThinkingContentStarted {
			return nil
		}
		if err := closeOpenBlock(); err != nil {
			return err
		}
		i.hasThinkingContentStarted = true
		startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: &MessageContentBlock{Type: "thinking", Thinking: lo.ToPtr(""), Signature: lo.ToPtr("")}}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_start event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_start", data))
		return nil
	}
	startTool := func(toolCall model.ToolCall) error {
		if i.toolCallIndices == nil {
			i.toolCallIndices = make(map[int]bool)
		}
		if i.toolCallIndices[toolCall.Index] && i.hasToolContentStarted {
			return nil
		}
		if err := closeOpenBlock(); err != nil {
			return err
		}
		i.toolCallIndices[toolCall.Index] = true
		i.hasToolContentStarted = true
		startBlock := &MessageContentBlock{Type: "tool_use", ID: toolCall.ID, Name: &toolCall.Function.Name, Input: json.RawMessage("{}")}
		if sig := toolCall.GetGeminiExtensions().ThoughtSignature; strings.TrimSpace(sig) != "" {
			compat.SaveGeminiThoughtSignatureScoped(i.geminiSignatureScope(ctx, i.modelName), toolCall.ID, toolCall.Function.Name, sig)
		}
		startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: startBlock}
		data, err := json.Marshal(startEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal content_block_start event: %w", err)
		}
		out = append(out, formatSSEEvent("content_block_start", data))
		return nil
	}

	for _, event := range events {
		if event.ID != "" {
			i.messageID = event.ID
		}
		if event.Model != "" {
			i.modelName = event.Model
		}
		switch event.Kind {
		case model.StreamEventKindMessageStart:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
		case model.StreamEventKindTextDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.Delta == nil || event.Delta.Text == "" {
				continue
			}
			if err := startText(); err != nil {
				return nil, err
			}
			text := event.Delta.Text
			deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("text_delta"), Text: &text}}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_delta", data))
		case model.StreamEventKindThinkingDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.Delta == nil {
				continue
			}
			if event.Delta.Thinking != "" {
				if err := startThinking(); err != nil {
					return nil, err
				}
				thinking := event.Delta.Thinking
				deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("thinking_delta"), Thinking: &thinking}}
				data, err := json.Marshal(deltaEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
				}
				out = append(out, formatSSEEvent("content_block_delta", data))
			}
			if signature := anthropicWireStreamSignature(event.Delta); signature != "" {
				if err := startThinking(); err != nil {
					return nil, err
				}
				deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("signature_delta"), Signature: &signature}}
				data, err := json.Marshal(deltaEvent)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal signature_delta event: %w", err)
				}
				out = append(out, formatSSEEvent("content_block_delta", data))
			}
		case model.StreamEventKindSignatureDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.Delta == nil {
				continue
			}
			signature := anthropicWireStreamSignature(event.Delta)
			if signature == "" {
				continue
			}
			if err := startThinking(); err != nil {
				return nil, err
			}
			deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("signature_delta"), Signature: &signature}}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal signature_delta event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_delta", data))
		case model.StreamEventKindContentBlockStart:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.ContentBlock == nil || event.ContentBlock.Type != "redacted_thinking" || event.ContentBlock.Data == "" {
				continue
			}
			if err := closeOpenBlock(); err != nil {
				return nil, err
			}
			startEvent := StreamEvent{Type: "content_block_start", Index: &i.contentIndex, ContentBlock: &MessageContentBlock{Type: "redacted_thinking", Data: event.ContentBlock.Data}}
			data, err := json.Marshal(startEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_start event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_start", data))
			stopEvent := StreamEvent{Type: "content_block_stop", Index: &i.contentIndex}
			stopData, err := json.Marshal(stopEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_stop", stopData))
			i.contentIndex++
		case model.StreamEventKindToolCallStart:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.ToolCall != nil {
				if err := startTool(*event.ToolCall); err != nil {
					return nil, err
				}
			}
		case model.StreamEventKindToolCallDelta:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if event.ToolCall == nil {
				continue
			}
			if err := startTool(*event.ToolCall); err != nil {
				return nil, err
			}
			arguments := event.ToolCall.Function.Arguments
			if event.Delta != nil && event.Delta.Arguments != "" {
				arguments = event.Delta.Arguments
			}
			if arguments == "" {
				continue
			}
			deltaEvent := StreamEvent{Type: "content_block_delta", Index: &i.contentIndex, Delta: &StreamDelta{Type: lo.ToPtr("input_json_delta"), PartialJSON: &arguments}}
			data, err := json.Marshal(deltaEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal content_block_delta event: %w", err)
			}
			out = append(out, formatSSEEvent("content_block_delta", data))
		case model.StreamEventKindToolCallStop, model.StreamEventKindContentBlockStop:
			if err := closeOpenBlock(); err != nil {
				return nil, err
			}
		case model.StreamEventKindMessageStop:
			if err := ensureStarted(event); err != nil {
				return nil, err
			}
			if err := closeOpenBlock(); err != nil {
				return nil, err
			}
			stopReason := event.StopReason.ToAnthropic()
			if stopReason == "" {
				stopReason = "end_turn"
			}
			i.stopReason = &stopReason
			i.stopSequence = event.StopSequence
			i.hasFinished = true
		case model.StreamEventKindUsageDelta:
			if event.Usage != nil && i.hasFinished && !i.messageStopped {
				finalEvents, err := i.finalizeStreamMessage(event.Usage)
				if err != nil {
					return nil, err
				}
				out = append(out, finalEvents...)
			}
		case model.StreamEventKindDone:
			if i.hasStarted && !i.messageStopped {
				if err := closeOpenBlock(); err != nil {
					return nil, err
				}
				i.ensureDefaultStopReason()
				i.hasFinished = true
				finalEvents, err := i.finalizeStreamMessage(i.pendingUsage)
				if err != nil {
					return nil, err
				}
				out = append(out, finalEvents...)
			}
		case model.StreamEventKindError:
			if event.Error == nil {
				continue
			}
			errType := event.Error.Detail.Type
			if errType == "" {
				errType = "api_error"
			}
			errPayload := StreamEvent{Type: "error", Error: &ErrorDetail{Type: errType, Message: event.Error.Detail.Message}}
			data, err := json.Marshal(errPayload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal error event: %w", err)
			}
			i.messageStopped = true
			out = append(out, formatSSEEvent("error", data))
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return joinSSEEvents(out), nil
}

func (i *MessagesInbound) hasOpenContentBlock() bool {
	return i.hasTextContentStarted || i.hasThinkingContentStarted || i.hasToolContentStarted
}

func (i *MessagesInbound) resetOpenContentState() {
	i.hasTextContentStarted = false
	i.hasThinkingContentStarted = false
	i.hasToolContentStarted = false
}

func (i *MessagesInbound) ensureDefaultStopReason() {
	if i.stopReason != nil {
		return
	}
	stopReason := "end_turn"
	if len(i.toolCallIndices) > 0 {
		stopReason = "tool_use"
	}
	i.stopReason = &stopReason
}

func (i *MessagesInbound) finalizeStreamAtEnd(usage *model.Usage) ([][]byte, error) {
	if i.messageStopped || !i.hasStarted {
		return nil, nil
	}

	var events [][]byte
	if i.hasOpenContentBlock() {
		stopEvent := StreamEvent{Type: "content_block_stop", Index: &i.contentIndex}
		data, err := json.Marshal(stopEvent)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal content_block_stop event: %w", err)
		}
		events = append(events, formatSSEEvent("content_block_stop", data))
		i.resetOpenContentState()
	}

	i.ensureDefaultStopReason()
	i.hasFinished = true
	terminalEvents, err := i.finalizeStreamMessage(usage)
	if err != nil {
		return nil, err
	}
	return append(events, terminalEvents...), nil
}

func (i *MessagesInbound) finalizeStreamMessage(usage *model.Usage) ([][]byte, error) {
	if i.messageStopped {
		return nil, nil
	}

	msgDeltaEvent := StreamEvent{
		Type: "message_delta",
	}

	if i.stopReason != nil {
		msgDeltaEvent.Delta = &StreamDelta{
			StopReason:   i.stopReason,
			StopSequence: i.stopSequence,
		}
	}

	if usage != nil {
		msgDeltaEvent.Usage = i.convertUsage(usage)
	}

	data, err := json.Marshal(msgDeltaEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message_delta event: %w", err)
	}

	msgStopEvent := StreamEvent{
		Type: "message_stop",
	}
	stopData, err := json.Marshal(msgStopEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message_stop event: %w", err)
	}

	i.messageStopped = true
	return [][]byte{
		formatSSEEvent("message_delta", data),
		formatSSEEvent("message_stop", stopData),
	}, nil
}

func joinSSEEvents(events [][]byte) []byte {
	result := make([]byte, 0)
	for idx, event := range events {
		if idx > 0 {
			result = append(result, '\n')
		}
		result = append(result, event...)
	}
	return result
}

// formatSSEEvent 格式化为完整的 SSE 事件格式
func formatSSEEvent(eventType string, data []byte) []byte {
	return []byte(fmt.Sprintf("event:%s\ndata:%s\n\n", eventType, string(data)))
}
