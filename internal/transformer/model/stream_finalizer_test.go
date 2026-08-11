package model

import (
	"errors"
	"testing"
)

func TestStreamFinalizerNormalizesLifecycleAndAggregates(t *testing.T) {
	finalizer := NewStreamFinalizer()
	events, err := finalizer.ProcessStreamEvents([]StreamEvent{
		{Kind: StreamEventKindTextDelta, ID: "resp_1", Model: "model_1", Delta: &StreamDelta{Text: "hello"}},
		{Kind: StreamEventKindMessageStop, ID: "resp_1", Model: "model_1"},
		{Kind: StreamEventKindDone},
	})
	if err != nil {
		t.Fatalf("ProcessStreamEvents() error = %v", err)
	}
	wantKinds := []StreamEventKind{StreamEventKindMessageStart, StreamEventKindTextDelta, StreamEventKindMessageStop, StreamEventKindUsageDelta, StreamEventKindDone}
	if len(events) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d: %+v", len(events), len(wantKinds), events)
	}
	for index, kind := range wantKinds {
		if events[index].Kind != kind {
			t.Fatalf("event[%d].Kind = %s, want %s", index, events[index].Kind, kind)
		}
	}
	if events[2].StopReason != FinishReasonStop {
		t.Fatalf("default stop reason = %s", events[2].StopReason)
	}

	result, err := finalizer.FinalizeStream()
	if err != nil {
		t.Fatalf("FinalizeStream() error = %v", err)
	}
	if len(result.TailEvents) != 0 {
		t.Fatalf("unexpected tail events: %+v", result.TailEvents)
	}
	if result.Response == nil || len(result.Response.Choices) != 1 || result.Response.Choices[0].Message == nil {
		t.Fatalf("unexpected aggregate: %+v", result.Response)
	}
	if got := result.Response.Choices[0].Message.Content.Content; got == nil || *got != "hello" {
		t.Fatalf("aggregated text = %v", got)
	}
}

func TestStreamFinalizerRejectsIncompleteAndProviderErrors(t *testing.T) {
	t.Run("missing finish reason", func(t *testing.T) {
		finalizer := NewStreamFinalizer()
		if _, err := finalizer.ProcessStreamEvents([]StreamEvent{{Kind: StreamEventKindTextDelta, Delta: &StreamDelta{Text: "partial"}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := finalizer.FinalizeStream(); !errors.Is(err, ErrStreamIncomplete) {
			t.Fatalf("FinalizeStream() error = %v, want ErrStreamIncomplete", err)
		}
	})

	t.Run("provider error", func(t *testing.T) {
		finalizer := NewStreamFinalizer()
		upstream := &ResponseError{StatusCode: 429, Detail: ErrorDetail{Message: "rate limited"}}
		if _, err := finalizer.ProcessStreamEvents([]StreamEvent{{Kind: StreamEventKindError, Error: upstream}}); !errors.Is(err, upstream) {
			t.Fatalf("ProcessStreamEvents() error = %v, want upstream error", err)
		}
	})
}

func TestStreamFinalizerPreservesUsageAfterStop(t *testing.T) {
	usage := &Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}
	finalizer := NewStreamFinalizer()
	events, err := finalizer.ProcessStreamEvents([]StreamEvent{
		{Kind: StreamEventKindMessageStart},
		{Kind: StreamEventKindMessageStop, StopReason: FinishReasonLength},
		{Kind: StreamEventKindUsageDelta, Usage: usage},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("unexpected normalized events: %+v", events)
	}
	result, err := finalizer.FinalizeStream()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TailEvents) != 1 || result.TailEvents[0].Kind != StreamEventKindDone {
		t.Fatalf("tail events = %+v, want done only", result.TailEvents)
	}
	if result.Usage != usage || result.FinishReasons[0] != FinishReasonLength {
		t.Fatalf("unexpected finalization: %+v", result)
	}
}

func TestStreamFinalizerDoneSynthesizesMissingStop(t *testing.T) {
	finalizer := NewStreamFinalizer()
	events, err := finalizer.ProcessStreamEvents([]StreamEvent{
		{Kind: StreamEventKindTextDelta, ID: "resp_1", Model: "model_1", Delta: &StreamDelta{Text: "hello"}},
		{Kind: StreamEventKindDone},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []StreamEventKind{
		StreamEventKindMessageStart,
		StreamEventKindTextDelta,
		StreamEventKindMessageStop,
		StreamEventKindUsageDelta,
		StreamEventKindDone,
	}
	if len(events) != len(wantKinds) {
		t.Fatalf("events = %+v, want kinds %v", events, wantKinds)
	}
	for index, kind := range wantKinds {
		if events[index].Kind != kind {
			t.Fatalf("event[%d].Kind = %s, want %s", index, events[index].Kind, kind)
		}
	}
	if events[2].StopReason != FinishReasonStop {
		t.Fatalf("synthesized stop reason = %s, want %s", events[2].StopReason, FinishReasonStop)
	}
}

func TestStreamFinalizerInfersToolFinishReasonPerChoice(t *testing.T) {
	finalizer := NewStreamFinalizer()
	events, err := finalizer.ProcessStreamEvents([]StreamEvent{
		{Kind: StreamEventKindTextDelta, Index: 0, Delta: &StreamDelta{Text: "text"}},
		{Kind: StreamEventKindToolCallStart, Index: 1, ToolCall: &ToolCall{Index: 0}},
		{Kind: StreamEventKindDone},
	})
	if err != nil {
		t.Fatal(err)
	}
	stops := make(map[int]FinishReason)
	for _, event := range events {
		if event.Kind == StreamEventKindMessageStop {
			stops[event.Index] = event.StopReason
		}
	}
	if stops[0] != FinishReasonStop || stops[1] != FinishReasonToolCalls {
		t.Fatalf("finish reasons = %#v, want choice 0 stop and choice 1 tool_calls", stops)
	}
}
