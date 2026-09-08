package outbound

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestProtocolDescriptorCapabilities(t *testing.T) {
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
	for _, typ := range []OutboundType{OutboundTypeOpenAIChat, OutboundTypeOpenAIResponse, OutboundTypeAnthropic, OutboundTypeGemini} {
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
	for _, invalid := range []OutboundType{-1, 4, 99} {
		if _, ok := Descriptor(invalid); ok || Get(invalid) != nil {
			t.Fatalf("invalid outbound type %d has a descriptor or factory", invalid)
		}
	}
	for want, typ := range map[int]OutboundType{0: OutboundTypeOpenAIChat, 1: OutboundTypeOpenAIResponse, 2: OutboundTypeAnthropic, 3: OutboundTypeGemini, 5: OutboundTypeOpenAIEmbedding} {
		if int(typ) != want {
			t.Fatalf("persisted outbound value changed: got %d, want %d", typ, want)
		}
	}
}

func TestProtocolDescriptorsHaveCompleteFactories(t *testing.T) {
	for outboundType, descriptor := range protocolDescriptors {
		if descriptor.Name == "" || descriptor.APIFormat == "" || descriptor.Transport == "" || descriptor.Factory == nil {
			t.Fatalf("descriptor %v is incomplete: %#v", outboundType, descriptor)
		}
		if got := Get(outboundType); got == nil {
			t.Fatalf("descriptor %v factory returned nil", outboundType)
		}
	}
	if !SupportsRelayOperation(OutboundTypeOpenAIChat, "images") {
		t.Fatal("OpenAI Chat descriptor must declare images relay operation")
	}
	if !SupportsRelayOperation(OutboundTypeOpenAIResponse, "responses/compact") {
		t.Fatal("OpenAI Responses descriptor must declare compact relay operation")
	}
	if !SupportsRelayOperation(OutboundTypeOpenAIResponse, RelayOperationResponsesWebSocket) {
		t.Fatal("OpenAI Responses descriptor must declare responses websocket relay operation")
	}
	if SupportsRelayOperation(OutboundTypeAnthropic, "images") {
		t.Fatal("Anthropic descriptor must not declare OpenAI images relay operation")
	}
}
