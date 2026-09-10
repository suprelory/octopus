package outbound

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

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
	if from := req.TransformerMetadataValue(model.TransformerMetadataAnthropicMaxTokensRepairFrom); from != "" {
		reportLoss(decision, "max_tokens", LossActionRepair, fmt.Sprintf("Anthropic repaired max_tokens=%s to 1 before canonical conversion", from))
	}
}

func degrade(decision *CapabilityDecision, field, reason string) {
	reportLoss(decision, field, LossActionDrop, reason)
}
