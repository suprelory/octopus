package relay

import (
	"strings"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// relayCapabilityCacheKey describes every decision input that can vary between
// candidates while one relay request is being processed. Channel identity is
// intentionally absent: the decision depends on the effective model, adapter
// type, passthrough result, and active override semantics, not the database ID.
type relayCapabilityCacheKey struct {
	effectiveModel string
	outboundType   outbound.OutboundType
	passthrough    bool
	overrideHash   string
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

	override := helper.InspectParamOverride(channel.ParamOverride)
	overrideConfigured := channelParamOverrideActive(channel)
	passthrough := planRelayPassthrough(p.request, p.rawBody, channel, adapter, p.websocketIngress, overrideConfigured)
	key := relayCapabilityCacheKey{
		effectiveModel: effectiveModel,
		outboundType:   channel.Type,
		passthrough:    passthrough,
		overrideHash:   override.Fingerprint,
	}
	if decision, ok := p.decisions[key]; ok {
		return decision
	}

	decision := outbound.PlanRequestForModel(p.request, effectiveModel, channel.Type, passthrough)
	decorateParamOverrideDecision(&decision, override, overrideConfigured)
	p.decisions[key] = decision
	return decision
}

func (p *relayCapabilityPlanner) rankChannel(channel *dbmodel.Channel, item dbmodel.GroupItem) int {
	if channel == nil || !channel.Enabled {
		return 3
	}
	adapter := outbound.Get(channel.Type)
	if adapter == nil || p == nil || p.request == nil {
		return 3
	}
	return capabilityRank(p.plan(channel, adapter, item.ModelName))
}

func channelParamOverrideConfigured(channel *dbmodel.Channel) bool {
	return channel != nil && channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != ""
}

// channelParamOverrideActive is narrower than configured: malformed, empty, or
// otherwise no-op documents are ignored by the helper for compatibility and do
// not justify disabling a byte-stable passthrough route.
func channelParamOverrideActive(channel *dbmodel.Channel) bool {
	if !channelParamOverrideConfigured(channel) {
		return false
	}
	inspection := helper.InspectParamOverride(channel.ParamOverride)
	return inspection.Valid && inspection.Active
}

func decorateParamOverrideDecision(decision *outbound.CapabilityDecision, inspection helper.ParamOverrideInspection, configured bool) {
	if decision == nil || !configured || !inspection.Valid || !inspection.Active {
		return
	}
	if !containsString(decision.RequiredFeatures, "param_override") {
		decision.RequiredFeatures = append(decision.RequiredFeatures, "param_override")
	}
	if !containsString(decision.ConversionPath, "wire_override") {
		decision.ConversionPath = append(decision.ConversionPath, "wire_override")
	}
	// Once bytes are patched after the protocol builder, native byte stability
	// can no longer be claimed even when ingress and egress formats match.
	if decision.StaticQuality == outbound.QualityNative {
		decision.StaticQuality = outbound.QualityConditional
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
