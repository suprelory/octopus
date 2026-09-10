package model

import (
	"errors"
	"testing"
)

func TestStreamTerminalPolicyFinishCauses(t *testing.T) {
	for _, tt := range []struct {
		name      string
		cause     StreamFinishCause
		allowEOF  bool
		stop      bool
		wantError bool
	}{
		{"explicit terminal closes choices", StreamFinishCauseExplicitTerminal, false, false, false},
		{"EOF rejects missing stop", StreamFinishCauseCleanEOF, false, false, true},
		{"EOF accepts finished choices", StreamFinishCauseCleanEOF, false, true, false},
		{"permissive EOF closes choices", StreamFinishCauseCleanEOF, true, false, false},
		{"source error overrides completed choices", StreamFinishCauseSourceError, true, true, true},
		{"cancellation overrides permissive EOF", StreamFinishCauseClientCancellation, true, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := NewStreamFinalizer(StreamTerminalPolicy{
				DefaultFinishReason:     FinishReasonLength,
				CleanEOFCompletes:       tt.allowEOF,
				RequiredLifecycleEvents: []StreamEventKind{StreamEventKindMessageStart, StreamEventKindMessageStop},
			})
			input := []StreamEvent{{Kind: StreamEventKindTextDelta, Index: 2, Delta: &StreamDelta{Text: "hello"}}}
			if tt.stop {
				input = append(input, StreamEvent{Kind: StreamEventKindMessageStop, Index: 2})
			}
			if _, err := f.ProcessStreamEvents(input); err != nil {
				t.Fatal(err)
			}
			result, err := f.Finish(tt.cause)
			if tt.wantError {
				if !errors.Is(err, ErrStreamIncomplete) || result != nil {
					t.Fatalf("Finish = %+v, %v", result, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if result.FinishCause != tt.cause || result.FinishReasons[2] != FinishReasonLength {
					t.Fatalf("Finish = %+v", result)
				}
				stops, done := 0, 0
				for _, event := range result.TailEvents {
					if event.Kind == StreamEventKindMessageStop {
						stops++
					}
					if event.Kind == StreamEventKindDone {
						done++
					}
				}
				wantStops := 1
				if tt.stop {
					wantStops = 0
				}
				if stops != wantStops || done != 1 {
					t.Fatalf("tail = %+v", result.TailEvents)
				}
			}
			if _, err := f.ProcessStreamEvents(input); !errors.Is(err, ErrStreamAlreadyFinalized) {
				t.Fatalf("Push after Finish = %v", err)
			}
		})
	}
}

func TestStreamTerminalPolicyDuplicateCompletionAndUsage(t *testing.T) {
	f := NewStreamFinalizer()
	usage := &Usage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10}
	events, err := f.ProcessStreamEvents([]StreamEvent{
		{Kind: StreamEventKindTextDelta, Delta: &StreamDelta{Text: "hello"}},
		{Kind: StreamEventKindToolCallStart, Index: 1, ToolCall: &ToolCall{ID: "call_1"}},
		{Kind: StreamEventKindMessageStop, StopReason: FinishReasonLength},
		{Kind: StreamEventKindMessageStop, StopReason: FinishReasonStop},
		{Kind: StreamEventKindUsageDelta, Usage: usage},
		{Kind: StreamEventKindDone, Terminal: true, TerminalEvent: "[DONE]"},
		{Kind: StreamEventKindDone, Terminal: true, TerminalEvent: "done"},
		{Kind: StreamEventKindMessageStop, StopReason: FinishReasonStop},
	})
	if err != nil {
		t.Fatal(err)
	}
	stops, done := 0, 0
	for _, event := range events {
		if event.Kind == StreamEventKindMessageStop {
			stops++
		}
		if event.Kind == StreamEventKindDone {
			done++
		}
	}
	if stops != 2 || done != 1 {
		t.Fatalf("duplicate completion: %+v", events)
	}
	result, err := f.FinalizeStream()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TailEvents) != 0 || result.Usage != usage || result.FinishReasons[0] != FinishReasonLength || result.FinishReasons[1] != FinishReasonToolCalls || result.FinishCause != StreamFinishCauseExplicitTerminal || result.TerminalEvent != "[DONE]" {
		t.Fatalf("finalization = %+v", result)
	}
}

func TestStreamTerminalPolicyCannotRecoverProviderError(t *testing.T) {
	f := NewStreamFinalizer(StreamTerminalPolicy{CleanEOFCompletes: true})
	upstream := &ResponseError{StatusCode: 502, Detail: ErrorDetail{Message: "provider failed"}}
	_, err := f.ProcessStreamEvents([]StreamEvent{
		{Kind: StreamEventKindMessageStart},
		{Kind: StreamEventKindMessageStop},
		{Kind: StreamEventKindDone},
		{Kind: StreamEventKindError, Terminal: true, Error: upstream},
	})
	if !errors.Is(err, upstream) {
		t.Fatalf("provider error = %v", err)
	}
	if result, err := f.FinalizeStream(); result != nil || !errors.Is(err, upstream) {
		t.Fatalf("Finish = %+v, %v", result, err)
	}
}
