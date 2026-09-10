package openai

import (
	"context"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestResponsesTerminalObservationMatchesCanonicalOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, wire string
		reason     model.FinishReason
		code       string
		failed     bool
	}{
		{name: "done", wire: `{"type":"response.done","response":{"status":"completed"}}`, reason: model.FinishReasonStop},
		{name: "done incomplete", wire: `{"type":"response.done","response":{"status":"incomplete"}}`, reason: model.FinishReasonLength},
		{name: "cancelled", wire: `{"type":"response.cancelled"}`, failed: true},
		{name: "canceled", wire: `{"type":"response.canceled"}`, failed: true},
		{name: "done failed", wire: `{"type":"response.done","response":{"status":"failed","error":{"code":"rate_limit","message":"retry later"}}}`, failed: true, code: "rate_limit"},
		{name: "nested error", wire: `{"type":"error","error":{"code":"invalid_request","message":"invalid"}}`, failed: true, code: "invalid_request"},
		{name: "numeric error", wire: `{"type":"response.error","code":429,"message":"retry"}`, failed: true, code: "429"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation, err := InspectResponseEvent([]byte(tc.wire), time.Now())
			if err != nil || !observation.Terminal || (observation.Error != nil) != tc.failed {
				t.Fatalf("observation = %+v, err = %v", observation, err)
			}
			events, err := (&ResponseOutbound{}).TransformSourceEvent(context.Background(), model.SourceEvent{Data: []byte(tc.wire)})
			if err != nil {
				t.Fatal(err)
			}
			var stop model.FinishReason
			var failure *model.ResponseError
			for _, event := range events {
				if event.Kind == model.StreamEventKindMessageStop {
					stop = event.StopReason
				}
				if event.Kind == model.StreamEventKindError {
					failure = event.Error
				}
			}
			if (failure != nil) != tc.failed || (!tc.failed && stop != tc.reason) {
				t.Fatalf("canonical = %+v, stop = %s, error = %v", events, stop, failure)
			}
			if tc.code != "" && (failure.Detail.Code != tc.code || observation.Error.Code != tc.code) {
				t.Fatalf("error codes disagree: %v / %v", failure, observation.Error)
			}
		})
	}
}
