package outbound

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestPlanRequestCoversRequestedSemantics(t *testing.T) {
	stream := true
	includeUsage := &model.StreamOptions{IncludeUsage: true}
	disableParallel := true
	req := &model.InternalLLMRequest{
		RequestType:     model.RequestTypeChat,
		RawAPIFormat:    model.APIFormatOpenAIChatCompletion,
		Model:           "claude-sonnet",
		Messages:        []model.Message{{Role: "user", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{{Type: "input_audio", Audio: &model.Audio{Format: "wav", Data: "AA=="}}}}}},
		Tools:           []model.Tool{{Type: "function", Function: model.Function{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		ToolChoice:      &model.ToolChoice{NamedToolChoice: &model.NamedToolChoice{Type: "function", DisableParallelToolUse: &disableParallel}},
		ReasoningEffort: "high",
		ResponseFormat:  &model.ResponseFormat{Type: "json_schema", Schema: &model.Schema{Type: "object"}},
		Stream:          &stream,
		StreamOptions:   includeUsage,
	}

	decision := PlanRequest(req, OutboundTypeAnthropic, false)
	if decision.Status != CapabilityDegraded {
		t.Fatalf("status = %s, want degraded: %+v", decision.Status, decision)
	}
	for _, feature := range []string{"tools", "tool_choice", "reasoning", "structured_output", "multimodal", "stream_usage"} {
		if !slices.Contains(decision.RequiredFeatures, feature) {
			t.Errorf("missing required feature %q in %v", feature, decision.RequiredFeatures)
		}
	}
	for _, field := range []string{"response_format", "messages[0].content[0]"} {
		if !slices.Contains(decision.DegradedFields, field) {
			t.Errorf("missing degraded field %q in %v", field, decision.DegradedFields)
		}
	}
}

func TestPlanRequestRejectsNativeResponsesSemantics(t *testing.T) {
	req := &model.InternalLLMRequest{RequestType: model.RequestTypeResponses, RawAPIFormat: model.APIFormatOpenAIResponse, Model: "gpt-5", Messages: []model.Message{{Role: "user"}}}
	req.MarkOpenAIResponsesPassthroughRequired("tool:web_search")
	decision := PlanRequest(req, OutboundTypeGemini, false)
	if decision.Status != CapabilityRejected {
		t.Fatalf("status = %s, want rejected", decision.Status)
	}
}

func TestPlanRequestNativePassthroughIsLossless(t *testing.T) {
	req := &model.InternalLLMRequest{RequestType: model.RequestTypeChat, RawAPIFormat: model.APIFormatAnthropicMessage, Model: "claude", Messages: []model.Message{{Role: "user"}}, ResponseFormat: &model.ResponseFormat{Type: "json_schema"}}
	decision := PlanRequest(req, OutboundTypeAnthropic, true)
	if decision.Status != CapabilitySupported || decision.Lossiness != "none" || !decision.Passthrough {
		t.Fatalf("unexpected passthrough decision: %+v", decision)
	}
	if !slices.Contains(decision.ConversionPath, "raw_passthrough") {
		t.Fatalf("conversion path = %v", decision.ConversionPath)
	}
}

func TestPlanRequestDetectsLossyGeminiSchema(t *testing.T) {
	req := &model.InternalLLMRequest{
		RequestType:  model.RequestTypeResponses,
		RawAPIFormat: model.APIFormatOpenAIResponse,
		Model:        "gemini-2.5-pro",
		Messages:     []model.Message{{Role: "user"}},
		ResponseFormat: &model.ResponseFormat{
			Type:   "json_schema",
			Schema: &model.Schema{Type: "object", Ref: "#/$defs/item"},
		},
	}
	decision := PlanRequest(req, OutboundTypeGemini, false)
	if decision.Status != CapabilityDegraded || !slices.Contains(decision.DegradedFields, "response_format.schema") {
		t.Fatalf("unexpected schema decision: %+v", decision)
	}
}

func TestPlanRequestReportsKnownFieldLosses(t *testing.T) {
	enabled := true
	summary := "auto"
	tests := []struct {
		name         string
		outboundType OutboundType
		request      *model.InternalLLMRequest
		wantFields   []string
	}{
		{
			name:         "chat non-function tool and reasoning controls",
			outboundType: OutboundTypeOpenAIChat,
			request: &model.InternalLLMRequest{
				Tools:                    []model.Tool{{Type: "image_generation"}},
				ReasoningBudget:          int64Ptr(100),
				AdaptiveThinking:         true,
				EnableThinking:           &enabled,
				ReasoningGenerateSummary: &summary,
			},
			wantFields: []string{"tools[0].type", "reasoning_budget", "adaptive_thinking", "enable_thinking", "reasoning_summary"},
		},
		{
			name:         "responses output modality",
			outboundType: OutboundTypeOpenAIResponse,
			request:      &model.InternalLLMRequest{Modalities: []string{"audio"}},
			wantFields:   []string{"modalities"},
		},
		{
			name:         "anthropic standalone reasoning budget",
			outboundType: OutboundTypeAnthropic,
			request:      &model.InternalLLMRequest{ReasoningBudget: int64Ptr(100)},
			wantFields:   []string{"reasoning_budget"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.request.RequestType = model.RequestTypeChat
			test.request.RawAPIFormat = model.APIFormatOpenAIChatCompletion
			decision := PlanRequest(test.request, test.outboundType, false)
			if decision.Status != CapabilityDegraded {
				t.Fatalf("decision = %#v, want degraded", decision)
			}
			for _, field := range test.wantFields {
				if !slices.Contains(decision.DegradedFields, field) {
					t.Errorf("missing degraded field %q in %#v", field, decision)
				}
			}
		})
	}
}

func int64Ptr(value int64) *int64 { return &value }
