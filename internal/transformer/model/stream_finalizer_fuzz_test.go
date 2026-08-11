package model

import (
	"errors"
	"testing"
)

func FuzzStreamFinalizerStateMachine(f *testing.F) {
	f.Add([]byte("text"))
	f.Add([]byte{0, 1, 2, 3, 4, 5, 255})
	f.Fuzz(func(t *testing.T, sequence []byte) {
		if len(sequence) > 4096 {
			t.Skip()
		}
		finalizer := NewStreamFinalizer()
		events := []StreamEvent{{Kind: StreamEventKindMessageStart, ID: "id", Model: "m", Role: "assistant"}}
		for index, value := range sequence {
			switch value % 5 {
			case 0:
				events = append(events, StreamEvent{Kind: StreamEventKindTextDelta, Index: 0, Delta: &StreamDelta{Text: string([]byte{value})}})
			case 1:
				events = append(events, StreamEvent{Kind: StreamEventKindThinkingDelta, Index: 0, Delta: &StreamDelta{Thinking: string([]byte{value})}})
			case 2:
				events = append(events, StreamEvent{Kind: StreamEventKindUsageDelta, Usage: &Usage{PromptTokens: int64(index), CompletionTokens: int64(value)}})
			case 3:
				events = append(events, StreamEvent{Kind: StreamEventKindToolCallDelta, Index: 0, ToolCall: &ToolCall{Index: 0, Function: FunctionCall{Arguments: string([]byte{value})}}})
			case 4:
				events = append(events, StreamEvent{Kind: StreamEventKindContentBlockStart, Index: 0, ContentBlock: &StreamContentBlock{Type: "opaque", Data: string([]byte{value})}})
			}
		}
		events = append(events, StreamEvent{Kind: StreamEventKindMessageStop, Index: 0, StopReason: FinishReasonStop})
		if _, err := finalizer.ProcessStreamEvents(events); err != nil {
			t.Fatal(err)
		}
		result, err := finalizer.FinalizeStream()
		if err != nil {
			t.Fatal(err)
		}
		if result.Response == nil || result.FinishReasons[0] != FinishReasonStop {
			t.Fatalf("invalid finalization: %#v", result)
		}
		if _, err := finalizer.FinalizeStream(); !errors.Is(err, ErrStreamAlreadyFinalized) {
			t.Fatalf("second finalization error = %v", err)
		}
	})
}
