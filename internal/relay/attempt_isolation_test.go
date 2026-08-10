package relay

import (
	"context"
	"net/url"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestNewAttemptRelayRequestIsolatesMutableState(t *testing.T) {
	rawBody := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hello"}]}`)
	base := &relayRequest{
		ctx:         context.Background(),
		inAdapter:   inbound.Get(inbound.InboundTypeOpenAIChat),
		inboundType: inbound.InboundTypeOpenAIChat,
		internalRequest: &transformerModel.InternalLLMRequest{
			Model:               "client-model",
			Query:               url.Values{"trace": {"base"}},
			TransformerMetadata: map[string]string{"source": "client"},
			Messages: []transformerModel.Message{{
				Role:    "user",
				Content: transformerModel.MessageContent{Content: stringPtr("hello")},
			}},
		},
		rawBody: rawBody,
	}

	first, err := newAttemptRelayRequest(base, context.Background(), "upstream-one")
	if err != nil {
		t.Fatalf("newAttemptRelayRequest(first) error = %v", err)
	}
	second, err := newAttemptRelayRequest(base, context.Background(), "upstream-two")
	if err != nil {
		t.Fatalf("newAttemptRelayRequest(second) error = %v", err)
	}

	if base.internalRequest.Model != "client-model" {
		t.Fatalf("base model = %q, want client-model", base.internalRequest.Model)
	}
	if first.internalRequest == base.internalRequest || second.internalRequest == base.internalRequest || first.internalRequest == second.internalRequest {
		t.Fatal("attempt requests share canonical request pointers")
	}
	if first.inAdapter == base.inAdapter || second.inAdapter == base.inAdapter || first.inAdapter == second.inAdapter {
		t.Fatal("attempts share inbound adapters")
	}
	if first.internalRequest.Model != "upstream-one" || second.internalRequest.Model != "upstream-two" {
		t.Fatalf("attempt models = %q, %q", first.internalRequest.Model, second.internalRequest.Model)
	}
	if first.streamPayloadWritten.Load() || second.streamPayloadWritten.Load() || first.responseCollected.Load() || second.responseCollected.Load() {
		t.Fatal("attempt atomic state must start clear")
	}

	first.internalRequest.Query.Set("trace", "first")
	first.internalRequest.TransformerMetadata["source"] = "first"
	*first.internalRequest.Messages[0].Content.Content = "changed"
	first.streamPayloadWritten.Store(true)
	first.responseCollected.Store(true)

	if got := base.internalRequest.Query.Get("trace"); got != "base" {
		t.Fatalf("base query mutated to %q", got)
	}
	if got := second.internalRequest.Query.Get("trace"); got != "base" {
		t.Fatalf("second attempt query mutated to %q", got)
	}
	if got := base.internalRequest.TransformerMetadata["source"]; got != "client" {
		t.Fatalf("base metadata mutated to %q", got)
	}
	if got := *second.internalRequest.Messages[0].Content.Content; got != "hello" {
		t.Fatalf("second attempt message mutated to %q", got)
	}
	if second.streamPayloadWritten.Load() || second.responseCollected.Load() || base.streamPayloadWritten.Load() || base.responseCollected.Load() {
		t.Fatal("attempt atomic state leaked to base or sibling attempt")
	}
}
