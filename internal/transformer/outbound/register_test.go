package outbound

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestEndpointCapabilities(t *testing.T) {
	tests := []struct {
		name        string
		outbound    OutboundType
		requestType model.RequestType
		format      model.APIFormat
	}{
		{"chat", OutboundTypeOpenAIChat, model.RequestTypeChat, model.APIFormatOpenAIChatCompletion},
		{"responses", OutboundTypeOpenAIResponse, model.RequestTypeResponses, model.APIFormatOpenAIResponse},
		{"anthropic", OutboundTypeAnthropic, model.RequestTypeChat, model.APIFormatAnthropicMessage},
		{"gemini", OutboundTypeGemini, model.RequestTypeChat, model.APIFormatGeminiContents},
		{"embedding", OutboundTypeOpenAIEmbedding, model.RequestTypeEmbedding, model.APIFormatOpenAIEmbedding},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !SupportsRequestType(tt.outbound, tt.requestType) {
				t.Fatalf("expected %v to support %q", tt.outbound, tt.requestType)
			}
			if !SupportsAPIFormat(tt.outbound, tt.format) {
				t.Fatalf("expected %v to expose %q", tt.outbound, tt.format)
			}
		})
	}

	if SupportsRequestType(OutboundTypeOpenAIEmbedding, model.RequestTypeChat) {
		t.Fatal("embedding endpoint must not accept chat requests")
	}
	for _, typ := range []OutboundType{OutboundTypeOpenAIChat, OutboundTypeOpenAIResponse, OutboundTypeAnthropic, OutboundTypeGemini, OutboundTypeVolcengine} {
		if !SupportsRequestType(typ, model.RequestTypeResponses) {
			t.Fatalf("%v must accept canonical Responses operations", typ)
		}
	}
	if SupportsRequestType(OutboundTypeOpenAIChat, model.RequestTypeEmbedding) {
		t.Fatal("chat endpoint must not accept embedding requests")
	}
	if !SupportsNativeFormat(OutboundTypeOpenAIResponse, model.APIFormatOpenAIResponse) {
		t.Fatal("OpenAI Responses endpoint must support native Responses input")
	}
	if SupportsNativeFormat(OutboundTypeVolcengine, model.APIFormatOpenAIResponse) {
		t.Fatal("Volcengine Responses-compatible endpoint must not claim byte-stable native passthrough")
	}
}
