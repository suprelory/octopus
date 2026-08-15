package relay

import (
	"reflect"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestRelayCapabilityPlannerCachesDecisionAndInvalidatesFingerprint(t *testing.T) {
	request := &transformerModel.InternalLLMRequest{
		RequestType:  transformerModel.RequestTypeChat,
		RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
		Model:        "request-model",
	}
	planner := newRelayCapabilityPlanner(request, []byte(`{}`), false)
	channel := &dbmodel.Channel{
		ID:              41,
		Type:            outbound.OutboundTypeAnthropic,
		PassthroughMode: dbmodel.ChannelPassthroughModeOff,
	}
	adapter := outbound.Get(channel.Type)

	first := planner.plan(channel, adapter, "claude-3")
	second := planner.plan(channel, adapter, "claude-3")
	if len(planner.decisions) != 1 {
		t.Fatalf("same candidate produced %d cached decisions, want 1", len(planner.decisions))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached decision changed:\nfirst=%#v\nsecond=%#v", first, second)
	}

	// A channel passthrough change changes the planner input and must not reuse
	// the canonical-conversion decision collected above.
	channel.PassthroughMode = dbmodel.ChannelPassthroughModeAuto
	third := planner.plan(channel, adapter, "claude-3")
	if len(planner.decisions) != 2 {
		t.Fatalf("passthrough change did not invalidate cache, entries=%d", len(planner.decisions))
	}
	if !third.Passthrough {
		t.Fatalf("updated passthrough configuration was not reflected: %#v", third)
	}

	// The selected model and outbound type are also part of the decision key.
	planner.plan(channel, adapter, "claude-4")
	if len(planner.decisions) != 3 {
		t.Fatalf("model change did not create a distinct cache entry, entries=%d", len(planner.decisions))
	}
	channel.Type = outbound.OutboundTypeGemini
	planner.plan(channel, outbound.Get(channel.Type), "claude-4")
	if len(planner.decisions) != 4 {
		t.Fatalf("outbound type change did not create a distinct cache entry, entries=%d", len(planner.decisions))
	}
}

func TestPlanRelayCapabilityLazilySharesRequestPlanner(t *testing.T) {
	request := &transformerModel.InternalLLMRequest{
		RequestType:  transformerModel.RequestTypeChat,
		RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
		Model:        "request-model",
	}
	req := &relayRequest{internalRequest: request, rawBody: []byte(`{}`)}
	channel := &dbmodel.Channel{ID: 42, Type: outbound.OutboundTypeAnthropic}
	adapter := outbound.Get(channel.Type)

	first := planRelayCapability(req, channel, adapter, "claude-3")
	second := planRelayCapability(req, channel, adapter, "claude-3")
	if req.capabilityPlanner == nil {
		t.Fatal("planRelayCapability did not attach a request-scoped planner")
	}
	if len(req.capabilityPlanner.decisions) != 1 {
		t.Fatalf("direct plan calls produced %d cached decisions, want 1", len(req.capabilityPlanner.decisions))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("direct cached decision changed:\nfirst=%#v\nsecond=%#v", first, second)
	}
}
