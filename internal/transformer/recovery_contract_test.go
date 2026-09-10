package transformer_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestNativeRequestRecoveryPreservesOrRejectsEveryRequiredSidecar(t *testing.T) {
	ctx := context.Background()
	req, err := inbound.Get(inbound.InboundTypeOpenAIResponse).TransformRequest(ctx, []byte(`{"model":"gpt-5","input":[{"type":"computer_call_output","call_id":"call_1","output":"ok","future_item":{"v":1}}],"tools":[{"type":"web_search","future_tool":{"v":2}}],"future_provider":{"v":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []bool{false, true} {
		request := req.Clone()
		if exact {
			request.MarkOpenAIExactReplayRequest()
		}
		decision := outbound.PlanRequestForModel(request, request.Model, outbound.OutboundTypeOpenAIResponse, false)
		if decision.Rejected() || decision.Conversion.Mode != model.ConversionRawSidecar || !decision.Conversion.RawInputPreserved || !decision.Conversion.ReplayAvailable || decision.Conversion.ExactReplay != exact {
			t.Fatalf("conversion = %+v", decision)
		}
		wire, _, err := outbound.BuildRequest(ctx, outbound.Get(outbound.OutboundTypeOpenAIResponse), outbound.OutboundTypeOpenAIResponse, request, "https://example.invalid", "test")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(wire.Body)
		wire.Body.Close()
		var payload map[string]json.RawMessage
		if json.Unmarshal(body, &payload) != nil || string(payload["future_provider"]) != `{"v":3}` {
			t.Fatalf("native fields lost: %s", body)
		}
		for _, field := range []string{"input", "tools"} {
			if len(payload[field]) == 0 {
				t.Fatalf("missing %s", field)
			}
		}
	}
	for _, edit := range []func(*model.InternalLLMRequest){
		func(r *model.InternalLLMRequest) {
			r.SetOpenAIRawInputItems(json.RawMessage(`[{"type":"message","role":"user","content":"wrong input"}]`))
		},
		func(r *model.InternalLLMRequest) {
			options := r.GetOpenAIResponsesOptions()
			options.RawTools = nil
			r.SetOpenAIResponsesOptions(options)
		},
		func(r *model.InternalLLMRequest) { delete(r.Operation.Recovery.Fields, "future_provider") },
	} {
		broken := req.Clone()
		edit(broken)
		decision := outbound.PlanRequestForModel(broken, broken.Model, outbound.OutboundTypeOpenAIResponse, false)
		if !decision.Rejected() || decision.Conversion.ReplayAvailable {
			t.Fatalf("missing native data accepted: %+v", decision)
		}
		if _, err := outbound.Get(outbound.OutboundTypeOpenAIResponse).TransformRequest(ctx, broken, "https://example.invalid", "test"); err == nil {
			t.Fatal("builder accepted a missing sidecar")
		}
	}
	if decision := outbound.PlanRequestForModel(req, req.Model, outbound.OutboundTypeGemini, false); !decision.Rejected() {
		t.Fatalf("cross-provider native loss accepted: %+v", decision)
	}
}

func TestConversionModesDistinguishCanonicalRawAndLossy(t *testing.T) {
	ctx := context.Background()
	req, err := inbound.Get(inbound.InboundTypeOpenAIChat).TransformRequest(ctx, []byte(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	decision := outbound.PlanRequestForModel(req, req.Model, outbound.OutboundTypeOpenAIChat, false)
	if decision.Conversion.Mode != model.ConversionLosslessCanonical || decision.Conversion.RawInputPreserved {
		t.Fatalf("canonical = %+v", decision)
	}
	topK := int64(5)
	req.TopK = &topK
	decision = outbound.PlanRequestForModel(req, req.Model, outbound.OutboundTypeOpenAIChat, false)
	if decision.Conversion.Mode != model.ConversionLossyCanonical || decision.Conversion.ReplayAvailable {
		t.Fatalf("lossy = %+v", decision)
	}
	request, err := inbound.Get(inbound.InboundTypeAnthropic).TransformRequest(ctx, []byte(`{"model":"claude","max_tokens":16,"messages":[{"role":"user","content":"hi"}],"future_provider":{"v":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	decision = outbound.PlanRequestForModel(request, request.Model, outbound.OutboundTypeAnthropic, true)
	if decision.Conversion.Mode != model.ConversionRawSidecar || !decision.Conversion.RawInputPreserved {
		t.Fatalf("raw = %+v", decision)
	}
	if _, err := outbound.Get(outbound.OutboundTypeAnthropic).TransformRequest(ctx, request, "https://example.invalid", "test"); err != nil {
		t.Fatal(err)
	}
	delete(request.Operation.Recovery.Fields, "future_provider")
	if _, err := outbound.Get(outbound.OutboundTypeAnthropic).TransformRequest(ctx, request, "https://example.invalid", "test"); err == nil {
		t.Fatal("missing provider extension accepted")
	}
}
