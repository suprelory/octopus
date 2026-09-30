package model

import (
	"context"
	"errors"
	"testing"
)

type converterParserFunc func(context.Context, SourceEvent) ([]StreamEvent, error)

func (p converterParserFunc) TransformSourceEvent(ctx context.Context, event SourceEvent) ([]StreamEvent, error) {
	return p(ctx, event)
}

func TestStreamConverterSealsEveryFinishCause(t *testing.T) {
	for _, cause := range []StreamFinishCause{StreamFinishCauseExplicitTerminal, StreamFinishCauseCleanEOF, StreamFinishCauseSourceError, StreamFinishCauseClientCancellation} {
		t.Run(string(cause), func(t *testing.T) {
			calls := 0
			parser := converterParserFunc(func(context.Context, SourceEvent) ([]StreamEvent, error) {
				calls++
				return []StreamEvent{{Kind: StreamEventKindTextDelta, Delta: &StreamDelta{Text: "hello"}}, {Kind: StreamEventKindMessageStop}}, nil
			})
			converter := NewStreamConverter(parser, DefaultStreamTerminalPolicy())
			if _, err := converter.Push(context.Background(), SourceEvent{}); err != nil {
				t.Fatal(err)
			}
			tail, err := converter.Finish(context.Background(), cause)
			failed := cause == StreamFinishCauseSourceError || cause == StreamFinishCauseClientCancellation
			if failed && (!errors.Is(err, ErrStreamIncomplete) || len(tail) != 0) {
				t.Fatalf("failed finish emitted events: %+v, %v", tail, err)
			}
			if !failed && (err != nil || len(tail) == 0) {
				t.Fatalf("successful finish = %+v, %v", tail, err)
			}
			if !converter.Completed() {
				t.Fatal("Finish did not seal converter")
			}
			if tail, _ := converter.Finish(context.Background(), cause); len(tail) != 0 {
				t.Fatalf("repeated Finish emitted %+v", tail)
			}
			if _, err := converter.Push(context.Background(), SourceEvent{}); !errors.Is(err, ErrStreamAlreadyFinalized) || calls != 1 {
				t.Fatalf("Push after Finish called parser: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestStreamConverterOwnsBlockAndTerminalLifecycles(t *testing.T) {
	parser := converterParserFunc(func(_ context.Context, event SourceEvent) ([]StreamEvent, error) {
		switch event.Type {
		case "delta":
			return []StreamEvent{{Kind: StreamEventKindMessageStart}, {Kind: StreamEventKindMessageStart}, {Kind: StreamEventKindToolCallDelta, ToolCall: &ToolCall{Index: 2, ID: "call", Function: FunctionCall{Name: "lookup", Arguments: `{}`}}}}, nil
		default:
			return []StreamEvent{{Kind: StreamEventKindMessageStop, Terminal: true, TerminalEvent: "response.completed"}, {Kind: StreamEventKindUsageDelta, Usage: &Usage{TotalTokens: 4}}}, nil
		}
	})
	c := NewStreamConverter(parser, DefaultStreamTerminalPolicy())
	start, err := c.Push(context.Background(), SourceEvent{Type: "delta"})
	if err != nil {
		t.Fatal(err)
	}
	end, err := c.Push(context.Background(), SourceEvent{Type: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := c.Push(context.Background(), SourceEvent{Type: "complete"}); err != nil || len(duplicate) != 0 {
		t.Fatalf("duplicate terminal = %+v, %v", duplicate, err)
	}
	want := []StreamEventKind{StreamEventKindMessageStart, StreamEventKindToolCallStart, StreamEventKindToolCallDelta, StreamEventKindToolCallStop, StreamEventKindMessageStop, StreamEventKindUsageDelta, StreamEventKindDone}
	all := append(start, end...)
	if len(all) != len(want) {
		t.Fatalf("events = %+v", all)
	}
	for index, kind := range want {
		if all[index].Kind != kind {
			t.Fatalf("event %d = %s, want %s", index, all[index].Kind, kind)
		}
	}
	if tail, err := c.Finish(context.Background(), StreamFinishCauseCleanEOF); err != nil || len(tail) != 0 {
		t.Fatalf("Finish = %+v, %v", tail, err)
	}
	result := c.Finalization()
	if result.Response.Choices[0].Message.ToolCalls[0].Function.Arguments != `{}` || result.FinishReasons[0] != FinishReasonToolCalls || result.Usage.TotalTokens != 4 {
		t.Fatalf("aggregate = %+v", result)
	}
}

func TestStreamConverterRejectsContentAfterChoiceStop(t *testing.T) {
	parser := converterParserFunc(func(context.Context, SourceEvent) ([]StreamEvent, error) {
		return []StreamEvent{{Kind: StreamEventKindMessageStop}, {Kind: StreamEventKindTextDelta, Delta: &StreamDelta{Text: "late"}}}, nil
	})
	c := NewStreamConverter(parser, DefaultStreamTerminalPolicy())
	if _, err := c.Push(context.Background(), SourceEvent{}); !errors.Is(err, ErrStreamAlreadyFinalized) {
		t.Fatalf("late content = %v", err)
	}
	if tail, err := c.Finish(context.Background(), StreamFinishCauseExplicitTerminal); err == nil || len(tail) != 0 {
		t.Fatalf("failed conversion recovered as success: %+v, %v", tail, err)
	}
}
