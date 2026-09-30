package model

import (
	"encoding/json"
	"testing"
)

func TestStreamAggregatorOwnsInputsAndSnapshots(t *testing.T) {
	text, finish := "A", "stop"
	annotationIndex := 2
	chunk := &InternalLLMResponse{
		ID: "first", Usage: &Usage{TotalTokens: 1},
		RawResponsesOutputItems: json.RawMessage(`[{"id":"item_1"}]`),
		Choices: []Choice{{Index: 3, FinishReason: &finish,
			Delta: &Message{Content: MessageContent{Content: &text}, Refusal: "no",
				ToolCalls: []ToolCall{{Index: 8, ID: "call_1", Function: FunctionCall{Name: "lookup", Arguments: "{"},
					ProviderExtensions: &ProviderExtensions{Common: &CommonExtension{Raw: json.RawMessage(`{"x":1}`)}}}}},
			Citations: []Citation{{AnnotationIndex: &annotationIndex, Raw: json.RawMessage(`{"type":"url"}`)}},
		}},
	}
	var aggregate StreamAggregator
	aggregate.Add(chunk)

	// Providers and callers may reuse a decoded chunk after Add returns.
	text, finish, annotationIndex = "B", "length", 7
	chunk.Usage.TotalTokens = 2
	chunk.RawResponsesOutputItems[0] = '?'
	chunk.Choices[0].Citations[0].Raw[0] = '?'
	chunk.Choices[0].Delta.ToolCalls[0].ProviderExtensions.Common.Raw[0] = '?'
	first := aggregate.Response()
	message := first.Choices[0].Message
	if *message.Content.Content != "A" || first.Usage.TotalTokens != 1 || *first.Choices[0].FinishReason != "stop" ||
		*first.Choices[0].Citations[0].AnnotationIndex != 2 || !json.Valid(first.RawResponsesOutputItems) ||
		!json.Valid(first.Choices[0].Citations[0].Raw) || !json.Valid(message.ToolCalls[0].ProviderExtensions.Common.Raw) {
		t.Fatalf("source mutation changed aggregate: %+v", first)
	}

	chunk.RawResponsesOutputItems = nil
	chunk.Choices[0].Citations = nil
	chunk.Choices[0].Delta.Refusal = " way"
	chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments = "}"
	chunk.Choices[0].Delta.ToolCalls[0].ProviderExtensions = nil
	aggregate.Add(chunk)
	if *message.Content.Content != "A" || message.ToolCalls[0].Function.Arguments != "{" {
		t.Fatal("adding another chunk changed an earlier snapshot")
	}
	// Snapshots must also be isolated from the retained accumulator.
	message.ToolCalls[0].ProviderExtensions.Common.Raw[0] = '?'
	first.Usage.TotalTokens = 100
	result := aggregate.BuildAndReset()
	message = result.Choices[0].Message
	if *message.Content.Content != "AB" || message.Refusal != "no way" || message.ToolCalls[0].Function.Arguments != "{}" ||
		!json.Valid(message.ToolCalls[0].ProviderExtensions.Common.Raw) || result.Usage.TotalTokens != 2 ||
		*result.Choices[0].FinishReason != "length" {
		t.Fatalf("unexpected accumulated response: %+v", result)
	}
	if aggregate.Response() != nil || aggregate.BuildAndReset() != nil {
		t.Fatal("BuildAndReset retained the stream")
	}
	aggregate.Add(&InternalLLMResponse{Choices: []Choice{{Delta: &Message{Content: MessageContent{Content: &text}}}}})
	if *aggregate.Response().Choices[0].Message.Content.Content != "B" || *message.Content.Content != "AB" {
		t.Fatal("reusing the aggregator changed the completed response")
	}
}

func TestStreamAggregatorMixedContentOrder(t *testing.T) {
	before, block, after, extra := "before", "block", "after", "!"
	index := 4
	var aggregate StreamAggregator
	for _, content := range []MessageContent{
		{Content: &before},
		{MultipleContent: []MessageContentPart{{Type: "text", BlockIndex: &index, Text: &block}}},
		{Content: &after},
		{MultipleContent: []MessageContentPart{{Type: "text", BlockIndex: &index, Text: &extra}}},
	} {
		aggregate.Add(&InternalLLMResponse{Choices: []Choice{{Delta: &Message{Content: content}}}})
	}
	content := aggregate.BuildAndReset().Choices[0].Message.Content
	if content.Content == nil || *content.Content != "beforeafter" || len(content.MultipleContent) != 3 ||
		*content.MultipleContent[0].Text != "before" || *content.MultipleContent[1].Text != "block!" ||
		*content.MultipleContent[2].Text != "after" {
		t.Fatalf("content order changed: %+v", content)
	}
}

func TestMergeToolCallDeltaDoesNotDuplicateFunctionName(t *testing.T) {
	toolCalls := []ToolCall{{
		Index: 0,
		ID:    "call_1",
		Type:  "function",
		Function: FunctionCall{
			Name: "Write",
		},
	}}

	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `{"file_path":`,
		},
	})
	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `"a.txt"}`,
		},
	})

	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "Write" {
		t.Fatalf("function name duplicated: %q", toolCalls[0].Function.Name)
	}
	if toolCalls[0].Function.Arguments != `{"file_path":"a.txt"}` {
		t.Fatalf("arguments not merged: %q", toolCalls[0].Function.Arguments)
	}
}

func TestMergeToolCallDeltaSetsFunctionNameWhenMissing(t *testing.T) {
	toolCalls := []ToolCall{{Index: 0, Type: "function"}}

	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name: "Search",
		},
	})

	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "Search" {
		t.Fatalf("function name not set: %q", toolCalls[0].Function.Name)
	}
}
