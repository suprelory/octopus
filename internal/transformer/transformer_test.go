package transformer

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/utils/tokenizer"
	"github.com/bestruirui/octopus/polywire/inbound"
)

func TestHostRetainsAnthropicTokenEstimator(t *testing.T) {
	request, err := Inbound(inbound.InboundTypeAnthropic).TransformRequest(context.Background(), []byte(`{"model":"claude-3-5-sonnet","max_tokens":16,"messages":[{"role":"user","content":"Hello Polywire"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := int64(tokenizer.CountTokens("Hello Polywire", "claude-3-5-sonnet"))
	if want <= 0 || request.EstimatedInputTokens != want {
		t.Fatalf("host estimate = %d, want %d", request.EstimatedInputTokens, want)
	}
}
