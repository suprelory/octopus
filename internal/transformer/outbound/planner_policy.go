package outbound

import (
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound/anthropic"
)

// LossAction describes what an outbound adapter does when a canonical field
// cannot be represented exactly. It is deliberately separate from
// CapabilityStatus: a request can remain routable after a drop or truncation.
type LossAction = model.RequestTransformationAction

const (
	LossActionPreserve  = model.RequestTransformationPreserve
	LossActionTranslate = model.RequestTransformationTranslate
	LossActionDrop      = model.RequestTransformationDrop
	LossActionTruncate  = model.RequestTransformationTruncate
	LossActionRepair    = model.RequestTransformationRepair
	LossActionReject    = model.RequestTransformationReject
)

// CapabilityLoss is a field-level explanation of a known conversion loss.
type CapabilityLoss struct {
	Field  string     `json:"field"`
	Action LossAction `json:"action"`
	Reason string     `json:"reason"`
}

// LossReport is the additive report emitted by the planner. Transformers can
// reuse the same shape later for runtime repair and truncation reports without
// changing the legacy DegradedFields/Reasons contract.
type LossReport []CapabilityLoss

// AdapterFieldPolicy is the declarative portion of an adapter's loss contract
// needed by the planner. A zero Limit means there is no cardinality bound.
type AdapterFieldPolicy struct {
	Action         LossAction
	Limit          int
	InboundFormats []model.APIFormat
}

// AdapterLossPolicy describes known field-level behavior for one outbound
// adapter. Fields absent from the map are outside this initial declarative
// coverage and may still be checked by feature-specific evaluators.
type AdapterLossPolicy struct {
	Fields map[string]AdapterFieldPolicy
}

const (
	lossFieldAudio            = "audio"
	lossFieldFrequencyPenalty = "frequency_penalty"
	lossFieldLogitBias        = "logit_bias"
	lossFieldLogprobs         = "logprobs"
	lossFieldMetadata         = "metadata"
	lossFieldPrediction       = "prediction"
	lossFieldPresencePenalty  = "presence_penalty"
	lossFieldSeed             = "seed"
	lossFieldStopSequences    = "stop"
	lossFieldTemperature      = "temperature"
	lossFieldTopK             = "top_k"
	lossFieldTopLogprobs      = "top_logprobs"
	lossFieldTopP             = "top_p"
	lossFieldUser             = "user"
	lossFieldWebSearchOptions = "web_search_options"
	lossFieldReasoningEffort  = "reasoning_effort"
)

// adapterLossPolicies mirrors behavior implemented by the wire builders. New
// whitelist drops and provider limits should be declared here before being
// handled by evaluateAdapterFieldLosses.
var adapterLossPolicies = map[OutboundType]AdapterLossPolicy{
	OutboundTypeOpenAIChat: {
		Fields: map[string]AdapterFieldPolicy{
			lossFieldTopK: {Action: LossActionDrop},
			// Anthropic stop_sequences are intentionally removed by the
			// Chat adapter because Chat's substring matching changes the
			// source protocol's semantics.
			lossFieldStopSequences: {
				Action:         LossActionDrop,
				InboundFormats: []model.APIFormat{model.APIFormatAnthropicMessage},
			},
		},
	},
	OutboundTypeOpenAIResponse: {
		Fields: map[string]AdapterFieldPolicy{
			lossFieldAudio:            {Action: LossActionDrop},
			lossFieldFrequencyPenalty: {Action: LossActionDrop},
			lossFieldLogitBias:        {Action: LossActionDrop},
			lossFieldLogprobs:         {Action: LossActionDrop},
			lossFieldPrediction:       {Action: LossActionDrop},
			lossFieldPresencePenalty:  {Action: LossActionDrop},
			lossFieldSeed:             {Action: LossActionDrop},
			lossFieldStopSequences:    {Action: LossActionDrop},
			lossFieldTopK:             {Action: LossActionDrop},
			lossFieldUser:             {Action: LossActionDrop},
			lossFieldWebSearchOptions: {Action: LossActionDrop},
		},
	},
	OutboundTypeAnthropic: {
		Fields: map[string]AdapterFieldPolicy{
			lossFieldAudio:            {Action: LossActionDrop},
			lossFieldFrequencyPenalty: {Action: LossActionDrop},
			lossFieldLogitBias:        {Action: LossActionDrop},
			lossFieldLogprobs:         {Action: LossActionDrop},
			lossFieldMetadata:         {Action: LossActionDrop},
			lossFieldPrediction:       {Action: LossActionDrop},
			lossFieldPresencePenalty:  {Action: LossActionDrop},
			lossFieldSeed:             {Action: LossActionDrop},
			lossFieldStopSequences:    {Action: LossActionTruncate, Limit: anthropic.MaxStopSequences},
			lossFieldTopLogprobs:      {Action: LossActionDrop},
			lossFieldWebSearchOptions: {Action: LossActionDrop},
		},
	},
	OutboundTypeGemini: {
		Fields: map[string]AdapterFieldPolicy{
			lossFieldLogitBias:        {Action: LossActionDrop},
			lossFieldPrediction:       {Action: LossActionDrop},
			lossFieldTopLogprobs:      {Action: LossActionTruncate, Limit: 5},
			lossFieldUser:             {Action: LossActionDrop},
			lossFieldWebSearchOptions: {Action: LossActionDrop},
		},
	},
}

// AdapterLossPolicies returns a defensive copy so callers cannot mutate
// planner behavior globally.
func AdapterLossPolicies(outboundType OutboundType) AdapterLossPolicy {
	policy := adapterLossPolicies[outboundType]
	if len(policy.Fields) == 0 {
		return AdapterLossPolicy{}
	}
	fields := make(map[string]AdapterFieldPolicy, len(policy.Fields))
	for field, fieldPolicy := range policy.Fields {
		fieldPolicy.InboundFormats = append([]model.APIFormat(nil), fieldPolicy.InboundFormats...)
		fields[field] = fieldPolicy
	}
	policy.Fields = fields
	return policy
}
