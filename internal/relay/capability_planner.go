package relay

import (
	"context"
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// relayCapabilityCacheKey describes every input that can change a decision
// while one relay request is being processed. The request itself and ingress
// mode are immutable for the lifetime of the planner.
type relayCapabilityCacheKey struct {
	channelID      int
	effectiveModel string
	outboundType   outbound.OutboundType
	passthrough    bool
}

// relayCapabilityPlanner is request-scoped. It is intentionally a regular map:
// ranking and candidate execution are sequential, and sharing this cache
// across requests would risk retaining request data and stale channel config.
type relayCapabilityPlanner struct {
	request          *model.InternalLLMRequest
	rawBody          []byte
	websocketIngress bool
	decisions        map[relayCapabilityCacheKey]outbound.CapabilityDecision
}

func newRelayCapabilityPlanner(request *model.InternalLLMRequest, rawBody []byte, websocketIngress bool) *relayCapabilityPlanner {
	return &relayCapabilityPlanner{
		request:          request,
		rawBody:          rawBody,
		websocketIngress: websocketIngress,
		decisions:        make(map[relayCapabilityCacheKey]outbound.CapabilityDecision),
	}
}

func (p *relayCapabilityPlanner) effectiveModel(modelName string) string {
	if p == nil || p.request == nil {
		return modelName
	}
	if strings.TrimSpace(modelName) == "" {
		return p.request.Model
	}
	return modelName
}

func (p *relayCapabilityPlanner) plan(channel *dbmodel.Channel, adapter model.Outbound, modelName string) outbound.CapabilityDecision {
	if p == nil || p.request == nil {
		return outbound.PlanRequestForModel(nil, "", outbound.OutboundType(0), false)
	}

	effectiveModel := p.effectiveModel(modelName)
	if channel == nil {
		return outbound.PlanRequestForModel(p.request, effectiveModel, outbound.OutboundType(-1), false)
	}
	if adapter == nil {
		// A nil adapter is an invalid/temporary candidate. Do not cache the
		// non-passthrough fallback in case a later validation supplies a real
		// adapter for the same channel and model.
		return outbound.PlanRequestForModel(p.request, effectiveModel, channel.Type, false)
	}

	passthrough := planRelayPassthrough(p.request, p.rawBody, channel, adapter, p.websocketIngress)
	key := relayCapabilityCacheKey{
		channelID:      channel.ID,
		effectiveModel: effectiveModel,
		outboundType:   channel.Type,
		passthrough:    passthrough,
	}
	if decision, ok := p.decisions[key]; ok {
		return decision
	}

	decision := outbound.PlanRequestForModel(p.request, effectiveModel, channel.Type, passthrough)
	p.decisions[key] = decision
	return decision
}

func (p *relayCapabilityPlanner) rank(ctx context.Context, item dbmodel.GroupItem) int {
	channel, err := op.ChannelGet(item.ChannelID, ctx)
	if err != nil || channel == nil || !channel.Enabled {
		return 3
	}
	adapter := outbound.Get(channel.Type)
	if adapter == nil || p == nil || p.request == nil {
		return 3
	}
	return capabilityRank(p.plan(channel, adapter, item.ModelName))
}
