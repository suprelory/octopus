package outbound

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func evaluateAdapterReportedChanges(req *model.InternalLLMRequest, effectiveModel string, outboundType OutboundType, decision *CapabilityDecision) {
	if req == nil || decision == nil {
		return
	}
	adapter := Get(outboundType)
	reporter, ok := adapter.(model.RequestChangeReporter)
	if !ok {
		return
	}
	for _, change := range reporter.DescribeRequestChanges(req, effectiveModel) {
		reportLoss(decision, change.Field, change.Action, change.Reason)
	}
}

// evaluateAdapterFieldLosses applies the adapter's declarative field policy
// to values actually present on the request. This keeps the planner honest:
// protocol support alone is not treated as lossless when a wire builder drops
// or bounds a populated field.
func evaluateAdapterFieldLosses(req *model.InternalLLMRequest, outboundType OutboundType, decision *CapabilityDecision) {
	if req == nil || decision == nil {
		return
	}
	if outboundType == OutboundTypeAnthropic {
		switch {
		case req.MaxTokens != nil && *req.MaxTokens < 1:
			reportLoss(decision, "max_tokens", LossActionRepair,
				fmt.Sprintf("Anthropic repairs max_tokens from %d to 1", *req.MaxTokens))
		case req.MaxTokens == nil && req.MaxCompletionTokens != nil && *req.MaxCompletionTokens < 1:
			reportLoss(decision, "max_completion_tokens", LossActionRepair,
				fmt.Sprintf("Anthropic repairs max_completion_tokens from %d to 1", *req.MaxCompletionTokens))
		}
	}
	policy := AdapterLossPolicies(outboundType)
	if len(policy.Fields) == 0 {
		return
	}

	if fieldPolicy, ok := policy.Fields[lossFieldTopK]; ok && req.TopK != nil && policyApplies(fieldPolicy, req.RawAPIFormat) {
		reportPolicyLoss(decision, lossFieldTopK, fieldPolicy, fmt.Sprintf("%s outbound does not preserve top_k", outboundType))
	}
	for _, field := range []struct {
		name    string
		present bool
	}{
		{name: lossFieldAudio, present: req.Audio != nil},
		{name: lossFieldFrequencyPenalty, present: req.FrequencyPenalty != nil},
		{name: lossFieldLogitBias, present: len(req.LogitBias) > 0},
		{name: lossFieldLogprobs, present: req.Logprobs != nil},
		{name: lossFieldMetadata, present: metadataHasAdapterLoss(req.Metadata, outboundType)},
		{name: lossFieldPrediction, present: len(req.Prediction) > 0},
		{name: lossFieldPresencePenalty, present: req.PresencePenalty != nil},
		{name: lossFieldSeed, present: req.Seed != nil},
		{name: lossFieldUser, present: req.User != nil},
		{name: lossFieldWebSearchOptions, present: len(req.WebSearchOptions) > 0},
	} {
		fieldPolicy, ok := policy.Fields[field.name]
		if !ok || !field.present || !policyApplies(fieldPolicy, req.RawAPIFormat) {
			continue
		}
		reason := fmt.Sprintf("%s outbound does not preserve %s", outboundType, field.name)
		if field.name == lossFieldMetadata && outboundType == OutboundTypeAnthropic {
			reason = "Anthropic outbound preserves only metadata.user_id"
		}
		reportPolicyLoss(decision, field.name, fieldPolicy, reason)
	}

	if fieldPolicy, ok := policy.Fields[lossFieldStopSequences]; ok && stopSequenceCount(req.Stop) > 0 && policyApplies(fieldPolicy, req.RawAPIFormat) {
		count := stopSequenceCount(req.Stop)
		switch fieldPolicy.Action {
		case LossActionTruncate:
			if fieldPolicy.Limit > 0 && count > fieldPolicy.Limit {
				reportPolicyLoss(decision, lossFieldStopSequences, fieldPolicy,
					fmt.Sprintf("%s outbound truncates stop_sequences from %d to %d entries", outboundType, count, fieldPolicy.Limit))
			}
		default:
			reportPolicyLoss(decision, lossFieldStopSequences, fieldPolicy,
				fmt.Sprintf("%s outbound does not preserve stop_sequences", outboundType))
		}
	}

	if fieldPolicy, ok := policy.Fields[lossFieldTopLogprobs]; ok && req.TopLogprobs != nil && policyApplies(fieldPolicy, req.RawAPIFormat) {
		switch fieldPolicy.Action {
		case LossActionTruncate:
			value := *req.TopLogprobs
			clamped := value
			if clamped < 0 {
				clamped = 0
			}
			if fieldPolicy.Limit > 0 && clamped > int64(fieldPolicy.Limit) {
				clamped = int64(fieldPolicy.Limit)
			}
			if clamped != value {
				reportPolicyLoss(decision, lossFieldTopLogprobs, fieldPolicy,
					fmt.Sprintf("%s outbound clamps top_logprobs from %d to %d", outboundType, value, clamped))
			}
		default:
			reportPolicyLoss(decision, lossFieldTopLogprobs, fieldPolicy,
				fmt.Sprintf("%s outbound does not preserve top_logprobs", outboundType))
		}
	}
	if outboundType == OutboundTypeAnthropic && req.User != nil {
		metadataUserID := strings.TrimSpace(req.Metadata["user_id"])
		providerUserID := req.TransformerMetadataValue(model.TransformerMetadataAnthropicUserID)
		effectiveUserID := metadataUserID
		if effectiveUserID == "" {
			effectiveUserID = providerUserID
		}
		if effectiveUserID != "" && effectiveUserID != strings.TrimSpace(*req.User) {
			reportLoss(decision, lossFieldUser, LossActionDrop,
				"Anthropic metadata.user_id overrides the generic user field")
		}
	}

	evaluateAnthropicThinkingFieldLosses(req, outboundType, decision)
}

func metadataHasAdapterLoss(metadata map[string]string, outboundType OutboundType) bool {
	if len(metadata) == 0 {
		return false
	}
	if outboundType != OutboundTypeAnthropic {
		return true
	}
	if len(metadata) != 1 {
		return true
	}
	userID, ok := metadata["user_id"]
	return !ok || strings.TrimSpace(userID) == ""
}

// evaluateAnthropicThinkingFieldLosses mirrors applyThinkingParamConstraints
// in the Anthropic adapter. Extended thinking forces temperature to 1 and
// removes top_p/top_k; reporting those deterministic changes lets strict
// capability policy reject them before the adapter silently repairs the body.
func evaluateAnthropicThinkingFieldLosses(req *model.InternalLLMRequest, outboundType OutboundType, decision *CapabilityDecision) {
	if outboundType != OutboundTypeAnthropic || req.ReasoningEffort == "" {
		return
	}
	if req.TopK != nil {
		reportLoss(decision, lossFieldTopK, LossActionDrop,
			"Anthropic removes top_k when extended thinking is enabled")
	}
	if req.TopP != nil {
		reportLoss(decision, lossFieldTopP, LossActionDrop,
			"Anthropic removes top_p when extended thinking is enabled")
	}
	if req.Temperature != nil && *req.Temperature != 1 {
		reportLoss(decision, lossFieldTemperature, LossActionRepair,
			"Anthropic forces temperature to 1 when extended thinking is enabled")
	}
}

func policyApplies(policy AdapterFieldPolicy, inboundFormat model.APIFormat) bool {
	if len(policy.InboundFormats) == 0 {
		return true
	}
	return slices.Contains(policy.InboundFormats, inboundFormat)
}

func stopSequenceCount(stop *model.Stop) int {
	if stop == nil {
		return 0
	}
	if stop.Stop != nil {
		return 1
	}
	return len(stop.MultipleStop)
}

func reportPolicyLoss(decision *CapabilityDecision, field string, policy AdapterFieldPolicy, reason string) {
	if policy.Action == "" || policy.Action == LossActionPreserve {
		return
	}
	reportLoss(decision, field, policy.Action, reason)
}

func reportLoss(decision *CapabilityDecision, field string, action LossAction, reason string) {
	if decision == nil || strings.TrimSpace(field) == "" || action == "" || action == LossActionPreserve {
		return
	}
	decision.DegradedFields = append(decision.DegradedFields, field)
	decision.Reasons = append(decision.Reasons, reason)
	decision.Losses = append(decision.Losses, CapabilityLoss{Field: field, Action: action, Reason: reason})
}

func evaluateInboundRepairs(req *model.InternalLLMRequest, decision *CapabilityDecision) {
	if req == nil || decision == nil {
		return
	}
	if repairedFrom := req.TransformerMetadataValue(model.TransformerMetadataAnthropicMaxTokensRepairFrom); repairedFrom != "" {
		reportLoss(decision, "max_tokens", LossActionRepair,
			fmt.Sprintf("Anthropic repaired max_tokens=%s to 1 before canonical conversion", repairedFrom))
	}
}

func degrade(decision *CapabilityDecision, field, reason string) {
	reportLoss(decision, field, LossActionDrop, reason)
}
