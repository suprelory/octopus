package model

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestStreamContentAggregationPreservesBlocksAcrossBatching(t *testing.T) {
	toolIndex, resultIndex, textIndex := 0, 3, 8
	citation := Citation{Provider: "anthropic", Type: "char_location", Raw: json.RawMessage(`{"type":"char_location","document_index":0,"start_char_index":0,"end_char_index":1,"cited_text":"x"}`)}
	events := []StreamEvent{
		{Kind: StreamEventKindMessageStart, Role: "assistant"},
		{Kind: StreamEventKindContentBlockStart, BlockIndex: &toolIndex, ContentBlock: &StreamContentBlock{Type: "mcp_tool_use", ServerToolUse: &ServerToolUseBlock{ID: "srv_1", Name: "lookup", BlockType: "mcp_tool_use", ServerName: "docs", Input: json.RawMessage("{}")}}},
		{Kind: StreamEventKindContentBlockDelta, BlockIndex: &toolIndex, ContentBlock: &StreamContentBlock{ServerToolUse: &ServerToolUseBlock{ID: "srv_1"}}, Delta: &StreamDelta{Arguments: `{"q":`}},
		{Kind: StreamEventKindContentBlockDelta, BlockIndex: &toolIndex, ContentBlock: &StreamContentBlock{ServerToolUse: &ServerToolUseBlock{ID: "srv_1"}}, Delta: &StreamDelta{Arguments: `"x"}`}},
		{Kind: StreamEventKindContentBlockStop, BlockIndex: &toolIndex},
		{Kind: StreamEventKindContentBlockStart, BlockIndex: &resultIndex, ContentBlock: &StreamContentBlock{Type: "mcp_tool_result", ServerToolResult: &ServerToolResultBlock{ToolUseID: "srv_1", Content: json.RawMessage(`{"type":"result","value":0}`)}}},
		{Kind: StreamEventKindContentBlockStop, BlockIndex: &resultIndex},
		{Kind: StreamEventKindTextDelta, BlockIndex: &textIndex, Delta: &StreamDelta{Text: "A"}},
		{Kind: StreamEventKindCitationDelta, BlockIndex: &textIndex, Delta: &StreamDelta{Citation: &citation}},
		{Kind: StreamEventKindTextDelta, BlockIndex: &textIndex, Delta: &StreamDelta{Text: "B"}},
		{Kind: StreamEventKindContentBlockStop, BlockIndex: &textIndex},
		{Kind: StreamEventKindMessageStop, StopReason: FinishReasonStop},
		{Kind: StreamEventKindDone},
	}
	for _, batchSize := range []int{1, 2, len(events)} {
		for _, bridge := range []bool{false, true} {
			name := "direct"
			if bridge {
				name = "legacy_bridge"
			}
			t.Run(fmt.Sprintf("%s/batch=%d", name, batchSize), func(t *testing.T) {
				var aggregator StreamAggregator
				for start := 0; start < len(events); start += batchSize {
					end := min(start+batchSize, len(events))
					chunk := InternalResponseFromStreamEvents(events[start:end])
					if bridge && chunk != nil && chunk.Object != "[DONE]" {
						chunk = InternalResponseFromStreamEvents(StreamEventsFromInternalResponse(chunk))
					}
					aggregator.Add(chunk)
				}
				response := aggregator.Response()
				assertNativeAggregate(t, response)
				// A response snapshot must not change the stored chunks or the next snapshot.
				response.Choices[0].Message.Content.MultipleContent[0].ServerToolUse.Input[0] = '?'
				response.Choices[0].Message.Content.MultipleContent[2].Citations[0].Raw[0] = '?'
				assertNativeAggregate(t, aggregator.Response())
			})
		}
	}
	if string(events[1].ContentBlock.ServerToolUse.Input) != "{}" || citation.Raw[0] != '{' {
		t.Fatal("aggregation mutated source events")
	}
}

func assertNativeAggregate(t *testing.T, response *InternalLLMResponse) {
	t.Helper()
	if response == nil || len(response.Choices) != 1 {
		t.Fatalf("missing aggregate: %+v", response)
	}
	choice := response.Choices[0]
	parts := choice.Message.Content.MultipleContent
	if len(parts) != 3 {
		t.Fatalf("content blocks lost or duplicated: %+v", parts)
	}
	use := parts[0].ServerToolUse
	if use == nil || string(use.Input) != `{"q":"x"}` || use.BlockType != "mcp_tool_use" || use.ServerName != "docs" || use.InputDelta != nil {
		t.Fatalf("server tool input changed: %+v", use)
	}
	if parts[0].BlockIndex == nil || *parts[0].BlockIndex != 0 || parts[1].ServerToolResult == nil || parts[1].ServerToolResult.BlockType != "mcp_tool_result" {
		t.Fatalf("native block identity lost: %+v", parts)
	}
	if parts[2].Text == nil || *parts[2].Text != "AB" || parts[2].BlockIndex == nil || *parts[2].BlockIndex != 8 {
		t.Fatalf("text fragments or source index changed: %+v", parts[2])
	}
	if len(parts[2].Citations) != 1 || !json.Valid(parts[2].Citations[0].Raw) || len(choice.Citations) != 1 {
		t.Fatalf("citations lost or duplicated: %+v", choice)
	}
	if choice.FinishReason == nil || *choice.FinishReason != FinishReasonStop.String() || len(choice.Message.ToolCalls) != 0 {
		t.Fatalf("native tool changed completion behavior: %+v", choice)
	}
}

func TestServerToolEventsCommitWithoutRequestingClientToolExecution(t *testing.T) {
	index := 0
	events := []StreamEvent{{
		Kind: StreamEventKindContentBlockStart, BlockIndex: &index,
		ContentBlock: &StreamContentBlock{Type: "server_tool_use", ServerToolUse: &ServerToolUseBlock{ID: "srv_1", Name: "web_search", Input: json.RawMessage("{}")}},
	}}
	if !HasSemanticStreamEvents(events) {
		t.Fatal("server tool invocation must cross the semantic commit boundary")
	}
	finalizer := NewStreamFinalizer()
	if _, err := finalizer.ProcessStreamEvents(events); err != nil {
		t.Fatal(err)
	}
	if _, err := finalizer.ProcessStreamEvents([]StreamEvent{{Kind: StreamEventKindDone}}); err != nil {
		t.Fatal(err)
	}
	finalized, err := finalizer.FinalizeStream()
	if err != nil {
		t.Fatal(err)
	}
	if finalized.FinishReasons[0] != FinishReasonStop || len(finalized.Response.Choices[0].Message.ToolCalls) != 0 {
		t.Fatalf("server-only stream requested client tool execution: %+v", finalized)
	}
}
