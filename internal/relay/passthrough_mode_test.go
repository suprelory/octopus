package relay

import (
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestShouldUseHTTPPassthroughHonorsChannelMode(t *testing.T) {
	attempt := &relayAttempt{
		relayRequest: &relayRequest{
			rawBody: []byte(`{"model":"m"}`),
			internalRequest: &transformerModel.InternalLLMRequest{
				RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
			},
		},
		channel:    &dbmodel.Channel{PassthroughMode: dbmodel.ChannelPassthroughModeAuto},
		outAdapter: outbound.Get(outbound.OutboundTypeAnthropic),
	}
	passthrough := attempt.outAdapter.(transformerModel.PassthroughCapable)
	if !attempt.shouldUseHTTPPassthrough(passthrough) {
		t.Fatal("auto mode must preserve same-format passthrough")
	}
	attempt.channel.PassthroughMode = dbmodel.ChannelPassthroughModeOff
	if attempt.shouldUseHTTPPassthrough(passthrough) {
		t.Fatal("off mode must force the transformer path")
	}
}

func TestShouldUseHTTPPassthroughKeepsResponsesNativeOnlySafety(t *testing.T) {
	request := &transformerModel.InternalLLMRequest{RawAPIFormat: transformerModel.APIFormatOpenAIResponse}
	request.MarkOpenAIResponsesPassthroughRequired("tool:custom")
	attempt := &relayAttempt{
		relayRequest: &relayRequest{rawBody: []byte(`{"model":"m"}`), internalRequest: request},
		channel:      &dbmodel.Channel{PassthroughMode: dbmodel.ChannelPassthroughModeOff},
		outAdapter:   outbound.Get(outbound.OutboundTypeOpenAIResponse),
	}
	passthrough := attempt.outAdapter.(transformerModel.PassthroughCapable)
	if !attempt.shouldUseHTTPPassthrough(passthrough) {
		t.Fatal("native-only Responses requests must override optional passthrough configuration")
	}
}
