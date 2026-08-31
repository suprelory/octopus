package anthropic

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/compat"
	"github.com/bestruirui/octopus/internal/transformer/model"
	anthropicModel "github.com/bestruirui/octopus/internal/transformer/protocol/anthropic"
)

func prepareAnthropicRequest(request *model.InternalLLMRequest, effectiveModel string) (*model.InternalLLMRequest, *anthropicModel.MessageRequest, []model.RequestTransformationChange) {
	if request == nil {
		return nil, nil, nil
	}
	prepared := request.Clone()
	if modelName := strings.TrimSpace(effectiveModel); modelName != "" {
		prepared.Model = modelName
	}
	prepared.NormalizeMessages()
	messages, alternation := model.EnforceAlternationWithReport(prepared.Messages, model.AlternationProviderAnthropic)
	prepared.Messages = messages
	messageCountBeforePatch := len(prepared.Messages)
	compat.PatchAnthropicRequest(prepared)

	wire := convertToAnthropicRequestUnpruned(prepared)
	changes := alternation.RequestChanges("Anthropic")
	if len(prepared.Messages) > messageCountBeforePatch {
		changes = append(changes, model.RequestTransformationChange{
			Field:  "messages",
			Action: model.RequestTransformationRepair,
			Reason: "Anthropic inserts synthetic tool_result blocks for orphaned tool calls",
		})
	}
	for _, field := range pruneCacheBreakpoints(wire) {
		changes = append(changes, model.RequestTransformationChange{
			Field:  field,
			Action: model.RequestTransformationTruncate,
			Reason: fmt.Sprintf("Anthropic keeps only the first %d cache_control breakpoints", model.AnthropicMaxCacheBreakpoints),
		})
	}
	return prepared, wire, changes
}

func (o *MessageOutbound) DescribeRequestChanges(request *model.InternalLLMRequest, effectiveModel string) []model.RequestTransformationChange {
	if request == nil {
		return nil
	}
	_, _, changes := prepareAnthropicRequest(request, effectiveModel)
	return changes
}
