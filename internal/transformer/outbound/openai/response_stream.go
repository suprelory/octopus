package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

func (o *ResponseOutbound) toolCallIndexFor(outputIndex int) int {
	if o.toolCallIndexes == nil {
		o.toolCallIndexes = make(map[int]int)
	}
	if idx, ok := o.toolCallIndexes[outputIndex]; ok {
		return idx
	}
	idx := len(o.toolCallIndexes)
	o.toolCallIndexes[outputIndex] = idx
	return idx
}

func (o *ResponseOutbound) toolCallFromItem(outputIndex int, item ResponsesItem) model.ToolCall {
	return model.ToolCall{
		Index: o.toolCallIndexFor(outputIndex),
		ID:    item.CallID,
		Type:  "function",
		Function: model.FunctionCall{
			Name:      item.Name,
			Namespace: item.Namespace,
		},
	}
}

func (o *ResponseOutbound) ensureToolCallStarted(base model.StreamEvent, outputIndex int) []model.StreamEvent {
	if o.toolCallStarted == nil {
		o.toolCallStarted = make(map[int]bool)
	}
	if o.toolCallStarted[outputIndex] {
		return nil
	}
	item, ok := o.outputItems[outputIndex]
	if !ok || item.Type != "function_call" || item.Name == "" {
		return nil
	}

	o.toolCallStarted[outputIndex] = true
	toolCall := o.toolCallFromItem(outputIndex, item)
	return []model.StreamEvent{{Kind: model.StreamEventKindToolCallStart, ID: base.ID, Model: base.Model, Index: base.Index, ToolCall: &toolCall}}
}

func (o *ResponseOutbound) handleFunctionCallArgumentsDone(base model.StreamEvent, event ResponsesStreamEvent) ([]model.StreamEvent, error) {
	item := o.ensureOutputItem(event.OutputIndex, "function_call")
	if item.Type != "function_call" {
		return nil, nil
	}

	identityChanged := false
	if event.CallID != "" && event.CallID != item.CallID {
		item.CallID = event.CallID
		identityChanged = true
	}
	if event.Name != "" && event.Name != item.Name {
		item.Name = event.Name
		identityChanged = true
	}
	if event.Namespace != "" && event.Namespace != item.Namespace {
		item.Namespace = event.Namespace
		identityChanged = true
	}

	finalArgs := event.Arguments
	if finalArgs == "" {
		finalArgs = item.Arguments
	}
	forwardedArgs := o.toolCallForwardedArguments[event.OutputIndex]
	missingArgs := ""
	if finalArgs != "" {
		switch {
		case forwardedArgs == "":
			missingArgs = finalArgs
		case strings.HasPrefix(finalArgs, forwardedArgs):
			missingArgs = strings.TrimPrefix(finalArgs, forwardedArgs)
		case equalJSONValues(forwardedArgs, finalArgs):
			// Some providers reformat the final JSON without changing its value.
			// The downstream has already received the complete arguments.
			missingArgs = ""
		default:
			callID := item.CallID
			if callID == "" {
				callID = fmt.Sprintf("output_index=%d", event.OutputIndex)
			}
			return nil, fmt.Errorf("function call arguments mismatch for call_id %q", callID)
		}
		item.Arguments = finalArgs
	}
	o.outputItems[event.OutputIndex] = item

	wasStarted := o.toolCallStarted[event.OutputIndex]
	events := o.ensureToolCallStarted(base, event.OutputIndex)
	if !o.toolCallStarted[event.OutputIndex] {
		// A function name is required before a tool call can be represented in
		// Chat-style streams. Keep the completed item for raw Responses replay.
		return events, nil
	}

	if missingArgs != "" || (identityChanged && wasStarted) {
		toolCall := o.toolCallFromItem(event.OutputIndex, item)
		var delta *model.StreamDelta
		if missingArgs != "" {
			delta = &model.StreamDelta{Arguments: missingArgs}
		}
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallDelta, ID: base.ID, Model: base.Model, Index: base.Index, ToolCall: &toolCall, Delta: delta})
	}
	if finalArgs != "" {
		o.toolCallForwardedArguments[event.OutputIndex] = finalArgs
	}
	return events, nil
}

func (o *ResponseOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	if len(eventData) == 0 {
		return nil, nil
	}
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return []model.StreamEvent{{Kind: model.StreamEventKindDone}}, nil
	}

	if !o.initialized {
		o.initialized = true
		o.outputItems = make(map[int]ResponsesItem)
		o.toolCallIndexes = make(map[int]int)
		o.toolCallStarted = make(map[int]bool)
		o.toolCallForwardedArguments = make(map[int]string)
	}

	var streamEvent ResponsesStreamEvent
	if err := json.Unmarshal(eventData, &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream event: %w", err)
	}

	if streamEvent.Response != nil {
		if streamEvent.Response.ID != "" {
			o.streamID = streamEvent.Response.ID
		}
		if streamEvent.Response.Model != "" {
			o.streamModel = streamEvent.Response.Model
		}
	}

	var events []model.StreamEvent
	base := model.StreamEvent{ID: o.streamID, Model: o.streamModel, Index: 0}

	switch streamEvent.Type {
	case "response.created", "response.in_progress":
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStart, ID: base.ID, Model: base.Model, Index: base.Index, Role: "assistant"})

	case "response.output_text.delta":
		o.mergeOutputTextDelta(streamEvent)
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindTextDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Text: streamEvent.Delta}})
		}

	case "response.function_call_arguments.delta":
		o.mergeFunctionCallDelta(streamEvent)
		item := o.outputItems[streamEvent.OutputIndex]
		if item.Name != "" {
			events = append(events, o.ensureToolCallStarted(base, streamEvent.OutputIndex)...)
			if streamEvent.Delta != "" {
				toolCall := o.toolCallFromItem(streamEvent.OutputIndex, item)
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindToolCallDelta, ID: base.ID, Model: base.Model, Index: base.Index, ToolCall: &toolCall, Delta: &model.StreamDelta{Arguments: streamEvent.Delta}})
				o.toolCallForwardedArguments[streamEvent.OutputIndex] += streamEvent.Delta
			}
		}

	case "response.function_call_arguments.done":
		doneEvents, err := o.handleFunctionCallArgumentsDone(base, streamEvent)
		if err != nil {
			return nil, err
		}
		events = append(events, doneEvents...)

	case "response.output_item.added":
		o.mergeOutputItemAdded(streamEvent)
		if item, ok := o.outputItems[streamEvent.OutputIndex]; ok && item.Type == "function_call" && item.Name != "" {
			events = append(events, o.ensureToolCallStarted(base, streamEvent.OutputIndex)...)
		}

	case "response.output_item.done":
		o.mergeOutputItemAdded(streamEvent)
		if streamEvent.Item != nil && streamEvent.Item.Type == "reasoning" && streamEvent.Item.EncryptedContent != nil && *streamEvent.Item.EncryptedContent != "" {
			signature := model.OpaqueSignature{
				Provider: model.SignatureProviderOpenAI,
				Kind:     model.OpaqueSignatureKindOpenAIReasoning,
				Value:    *streamEvent.Item.EncryptedContent,
			}
			events = append(events, model.StreamEvent{
				Kind:  model.StreamEventKindSignatureDelta,
				ID:    base.ID,
				Model: base.Model,
				Index: base.Index,
				Delta: &model.StreamDelta{Signature: signature.Value, SignatureSource: &signature},
			})
		}

	case "response.reasoning_summary_text.delta":
		o.mergeReasoningDelta(streamEvent)
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindThinkingDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Thinking: streamEvent.Delta}})
		}

	case "response.reasoning_text.delta":
		o.mergeReasoningTextDelta(streamEvent)
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindThinkingDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Thinking: streamEvent.Delta}})
		}

	case "response.reasoning_text.done":
		o.mergeReasoningTextDone(streamEvent)
		return nil, nil

	case "response.refusal.delta":
		if streamEvent.Delta != "" {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindTextDelta, ID: base.ID, Model: base.Model, Index: base.Index, Delta: &model.StreamDelta{Refusal: streamEvent.Delta}})
		}

	case "response.refusal.done":
		return nil, nil

	case "response.completed":
		if streamEvent.Response != nil {
			if len(streamEvent.Response.Output) > 0 {
				if rawOutput, marshalErr := json.Marshal(sanitizeResponsesItems(streamEvent.Response.Output)); marshalErr == nil {
					base.ProviderExtensions = &model.ProviderExtensions{OpenAI: &model.OpenAIExtension{RawResponseItems: rawOutput}}
				}
			} else if rawOutput, ok := o.marshalTrackedOutputItems(); ok {
				base.ProviderExtensions = &model.ProviderExtensions{OpenAI: &model.OpenAIExtension{RawResponseItems: rawOutput}}
			}
			finishReason, respErr := normalizeResponsesFinishReason(streamEvent.Response.Status, streamEvent.Response.Error)
			if respErr != nil {
				events = append(events, model.StreamEvent{Kind: model.StreamEventKindError, ID: base.ID, Model: base.Model, Error: respErr})
				return events, nil
			}
			if finishReason != nil && *finishReason == "stop" && o.responseCarriesFunctionCall(streamEvent.Response) {
				finishReason = lo.ToPtr("tool_calls")
			}
			stopEvent := model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: base.ID, Model: base.Model, Index: base.Index, StopReason: model.ParseFinishReason(lo.FromPtr(finishReason)), ProviderExtensions: base.ProviderExtensions}
			events = append(events, stopEvent)
			if streamEvent.Response.Usage != nil {
				usage := convertResponsesUsage(streamEvent.Response.Usage)
				usageEvent := model.StreamEvent{Kind: model.StreamEventKindUsageDelta, ID: base.ID, Model: base.Model, Usage: usage, ProviderExtensions: base.ProviderExtensions}
				events = append(events, usageEvent)
			}
		}

	case "response.failed", "response.incomplete", "error":
		var reason *string
		var respErr *model.ResponseError
		switch streamEvent.Type {
		case "response.incomplete":
			reason = lo.ToPtr("length")
		default:
			reason = lo.ToPtr("stop")
		}
		if streamEvent.Response != nil && streamEvent.Response.Error != nil {
			respErr = &model.ResponseError{
				Detail: model.ErrorDetail{
					Code:    fmt.Sprintf("%d", streamEvent.Response.Error.Code),
					Message: streamEvent.Response.Error.Message,
				},
			}
		} else if streamEvent.Code != "" || streamEvent.Message != "" {
			respErr = &model.ResponseError{
				Detail: model.ErrorDetail{
					Code:    streamEvent.Code,
					Message: streamEvent.Message,
				},
			}
		}
		if respErr != nil {
			events = append(events, model.StreamEvent{Kind: model.StreamEventKindError, ID: base.ID, Model: base.Model, Error: respErr})
		}
		events = append(events, model.StreamEvent{Kind: model.StreamEventKindMessageStop, ID: base.ID, Model: base.Model, Index: base.Index, StopReason: model.ParseFinishReason(lo.FromPtr(reason))})

	default:
		return nil, nil
	}

	return events, nil
}

func (o *ResponseOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	events, err := o.TransformStreamEvent(ctx, eventData)
	if err != nil {
		return nil, err
	}
	return model.InternalResponseFromStreamEvents(events), nil
}
