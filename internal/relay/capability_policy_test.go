package relay

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	transformermodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestEvaluateCapabilityPolicy(t *testing.T) {
	tests := []struct {
		name       string
		status     outbound.CapabilityStatus
		policy     capabilityDegradationPolicy
		wantReject bool
		wantCode   string
	}{
		{
			name:       "supported allow",
			status:     outbound.CapabilitySupported,
			policy:     capabilityPolicyAllow,
			wantReject: false,
		},
		{
			name:       "degraded allow",
			status:     outbound.CapabilityDegraded,
			policy:     capabilityPolicyAllow,
			wantReject: false,
		},
		{
			name:       "degraded warn",
			status:     outbound.CapabilityDegraded,
			policy:     capabilityPolicyWarn,
			wantReject: false,
		},
		{
			name:       "degraded strict",
			status:     outbound.CapabilityDegraded,
			policy:     capabilityPolicyStrict,
			wantReject: true,
			wantCode:   CodeRelayCapabilityRejected,
		},
		{
			name:       "rejected allow",
			status:     outbound.CapabilityRejected,
			policy:     capabilityPolicyAllow,
			wantReject: true,
			wantCode:   CodeRelayModelNotSupported,
		},
		{
			name:       "rejected strict",
			status:     outbound.CapabilityRejected,
			policy:     capabilityPolicyStrict,
			wantReject: true,
			wantCode:   CodeRelayModelNotSupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reject, code := evaluateCapabilityPolicy(outbound.CapabilityDecision{Status: tt.status}, tt.policy)
			if reject != tt.wantReject || code != tt.wantCode {
				t.Fatalf("evaluateCapabilityPolicy() = (%t, %q), want (%t, %q)", reject, code, tt.wantReject, tt.wantCode)
			}
			if got := shouldRejectCapability(outbound.CapabilityDecision{Status: tt.status}, tt.policy); got != tt.wantReject {
				t.Fatalf("shouldRejectCapability() = %t, want %t", got, tt.wantReject)
			}
		})
	}
}

func TestCapabilityTraceAndRejectionMessagePreserveDecisionDetails(t *testing.T) {
	decision := outbound.CapabilityDecision{
		Status:         outbound.CapabilityDegraded,
		OutboundFormat: transformermodel.APIFormatAnthropicMessage,
		ConversionPath: []string{"openai/chat_completions", "canonical", "anthropic/messages"},
		DegradedFields: []string{"max_tokens"},
		Losses: outbound.LossReport{{
			Field:  "max_tokens",
			Action: outbound.LossActionRepair,
			Reason: "repaired",
		}},
		Lossiness: "known",
		Reasons:   []string{"repaired"},
	}

	trace := capabilityTrace(decision, capabilityPolicyStrict, "anthropic")
	if trace.Policy != "strict" || trace.AdapterType != "anthropic" || len(trace.Losses) != 1 {
		t.Fatalf("incomplete capability trace: %#v", trace)
	}
	message := capabilityRejectionMessage(decision, "anthropic")
	for _, fragment := range []string{"fields=max_tokens", "target=anthropic/messages", "path=openai/chat_completions -> canonical -> anthropic/messages"} {
		if !strings.Contains(message, fragment) {
			t.Fatalf("rejection message %q missing %q", message, fragment)
		}
	}
}

func TestResolveFinalAttemptResultDoesNotMaskSupportedFailure(t *testing.T) {
	upstreamErr := errors.New("upstream failed")
	upstreamResult := attemptResult{Err: upstreamErr, StatusCode: http.StatusServiceUnavailable}
	capabilityErr := errors.New("capability rejected")
	capabilityResult := attemptResult{
		Err:           capabilityErr,
		StatusCode:    http.StatusBadRequest,
		ProtocolError: relayProtocolError(http.StatusBadRequest, CodeRelayCapabilityRejected, capabilityErr.Error()),
	}

	gotResult, gotErr := resolveFinalAttemptResult(true, upstreamErr, upstreamResult, capabilityErr, capabilityResult)
	if !errors.Is(gotErr, upstreamErr) || gotResult.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("supported failure was masked: err=%v result=%+v", gotErr, gotResult)
	}
}

func TestResolveFinalAttemptResultUsesCapabilityWhenAllCandidatesReject(t *testing.T) {
	capabilityErr := errors.New("capability rejected")
	capabilityResult := attemptResult{
		Err:           capabilityErr,
		StatusCode:    http.StatusBadRequest,
		ProtocolError: relayProtocolError(http.StatusBadRequest, CodeRelayCapabilityRejected, capabilityErr.Error()),
	}

	gotResult, gotErr := resolveFinalAttemptResult(false, nil, attemptResult{}, capabilityErr, capabilityResult)
	if !errors.Is(gotErr, capabilityErr) || gotResult.ProtocolError == nil || gotResult.ProtocolError.Detail.Code != CodeRelayCapabilityRejected {
		t.Fatalf("capability rejection was not selected: err=%v result=%+v", gotErr, gotResult)
	}
}

func TestPreferCapabilityRejectionUsesStablePriority(t *testing.T) {
	hardErr := errors.New("unsupported request type")
	hardResult := attemptResult{
		Err:           hardErr,
		StatusCode:    http.StatusBadRequest,
		ProtocolError: relayProtocolError(http.StatusBadRequest, CodeRelayModelNotSupported, hardErr.Error()),
	}
	strictErr := errors.New("strict degradation")
	strictResult := attemptResult{
		Err:           strictErr,
		StatusCode:    http.StatusBadRequest,
		ProtocolError: relayProtocolError(http.StatusBadRequest, CodeRelayCapabilityRejected, strictErr.Error()),
	}

	gotResult, gotErr := preferCapabilityRejection(strictErr, strictResult, hardErr, hardResult)
	if !errors.Is(gotErr, hardErr) || gotResult.ProtocolError.Detail.Code != CodeRelayModelNotSupported {
		t.Fatalf("hard rejection did not replace strict degradation: err=%v result=%+v", gotErr, gotResult)
	}

	gotResult, gotErr = preferCapabilityRejection(hardErr, hardResult, strictErr, strictResult)
	if !errors.Is(gotErr, hardErr) || gotResult.ProtocolError.Detail.Code != CodeRelayModelNotSupported {
		t.Fatalf("strict degradation replaced hard rejection: err=%v result=%+v", gotErr, gotResult)
	}
}
