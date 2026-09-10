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
	index := o.streamReasoningIndex[candidateIndex]
	o.streamReasoningIndex[candidateIndex]++
	return index
}

func (o *MessagesOutbound) nextToolCallIndex() int {
	index := o.streamToolCallIndex
	o.streamToolCallIndex++
	return index
}

func (o *MessagesOutbound) TransformSourceEvent(ctx context.Context, event model.SourceEvent) ([]model.StreamEvent, error) {
	eventType := strings.TrimSpace(event.Type)
	if bytes.Equal(bytes.TrimSpace(event.Data), []byte("[DONE]")) || eventType == "[DONE]" || strings.EqualFold(eventType, "done") {
		if eventType == "" {
			eventType = "[DONE]"
		}
		return []model.StreamEvent{{Kind: model.StreamEventKindDone, Terminal: true, TerminalEvent: eventType}}, nil
	}
	if len(bytes.TrimSpace(event.Data)) == 0 {
		return nil, nil
	}
	var response model.GeminiGenerateContentResponse
	if err := json.Unmarshal(event.Data, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gemini stream chunk: %w", err)
	}
	chunk := convertGeminiToLLMResponse(&response, true, o.nextReasoningIndex, o.nextToolCallIndex)
	var events []model.StreamEvent
	for _, event := range model.StreamEventsFromInternalResponse(chunk) {
		if event.Kind == model.StreamEventKindToolCallDelta && event.ToolCall != nil {
			start := event
			start.Kind, start.Delta = model.StreamEventKindToolCallStart, nil
			tool := *event.ToolCall
			tool.Function.Arguments = ""
			start.ToolCall = &tool
			events = append(events, start, event)
			start.Kind = model.StreamEventKindToolCallStop
			events = append(events, start)
		} else {
			events = append(events, event)
		}
	}
	if len(response.Candidates) == 0 && response.PromptFeedback != nil && response.PromptFeedback.BlockReason != "" {
		for index := range events {
			if events[index].Kind == model.StreamEventKindMessageStop {
				events[index].Terminal, events[index].TerminalEvent = true, "prompt_feedback"
			}
		}
	}
	return events, nil
}

func (o *MessagesOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	return o.TransformSourceEvent(ctx, model.SourceEvent{Data: eventData})
}

func (o *MessagesOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	events, err := o.TransformSourceEvent(ctx, model.SourceEvent{Data: eventData})
	if err != nil {
		return nil, err
	}
	return model.InternalResponseFromStreamEvents(events), nil
}
