package outbound

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	geminiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/gemini"
)

func evaluateFeature(req *model.InternalLLMRequest, effectiveModel string, outboundType OutboundType, feature SemanticFeature, decision *CapabilityDecision) {
	switch feature {
	case FeatureStructuredOutput:
		switch outboundType {
		case OutboundTypeAnthropic:
			degrade(decision, "response_format", "Anthropic transformer cannot emit the canonical response_format body")
		case OutboundTypeGemini:
			if req.ResponseFormat != nil && req.ResponseFormat.Schema != nil {
				if _, err := req.ResponseFormat.Schema.ToGemini(); errors.Is(err, model.ErrSchemaLossy) {
					reportLoss(decision, "response_format.schema", LossActionTranslate, err.Error())
				}
			}
		}
	case FeatureToolChoice:
		if outboundType == OutboundTypeAnthropic && req.ToolChoice != nil && req.ToolChoice.NamedToolChoice != nil {
			named := req.ToolChoice.NamedToolChoice
			typ := strings.ToLower(strings.TrimSpace(named.Type))
			if (typ == "tool" || typ == "function") && strings.TrimSpace(named.ResolvedFunctionName()) == "" {
				reportLoss(decision, "tool_choice.name", LossActionRepair,
					"Anthropic requires a tool name; the outbound adapter repairs this choice to auto")
			}
		}
		if req.ToolChoice != nil && req.ToolChoice.NamedToolChoice != nil && req.ToolChoice.NamedToolChoice.DisableParallelToolUse != nil && outboundType != OutboundTypeAnthropic {
			degrade(decision, "tool_choice.disable_parallel_tool_use", "target protocol has no equivalent disable_parallel_tool_use control")
		}
	case FeatureReasoning:
		evaluateReasoning(req, outboundType, decision)
		if outboundType == OutboundTypeGemini {
			for _, change := range geminiOutbound.DescribeThinkingConfigChanges(effectiveModel, req.ReasoningBudget, req.ReasoningEffort, req.AdaptiveThinking) {
				action := LossActionRepair
				if change.Dropped {
					action = LossActionDrop
				} else if change.Translated {
					action = LossActionTranslate
				}
				reportLoss(decision, change.Field, action, change.Reason)
			}
		}
	case FeatureTools:
		for index, tool := range req.Tools {
			if supportsToolType(outboundType, tool) {
				continue
			}
			degrade(decision, fmt.Sprintf("tools[%d].type", index), fmt.Sprintf("tool type %q is not representable on %s", tool.Type, outboundType))
		}
	case FeatureMultimodal:
		evaluateMultimodal(req, outboundType, decision)
	case FeatureStreamUsage:
		// Every streaming chat adapter is required to expose canonical usage.
	}
}

func supportsToolType(outboundType OutboundType, tool model.Tool) bool {
	typ := strings.ToLower(strings.TrimSpace(tool.Type))
	if typ == "" || typ == "function" {
		return true
	}
	switch outboundType {
	case OutboundTypeOpenAIResponse:
		return typ == "image_generation"
	case OutboundTypeGemini:
		return slices.Contains([]string{"server_search", "code_execution", "url_context"}, typ)
	case OutboundTypeAnthropic:
		return len(tool.AnthropicServerSpec) > 0
	default:
		return false
	}
}

func evaluateMultimodal(req *model.InternalLLMRequest, outboundType OutboundType, decision *CapabilityDecision) {
	for messageIndex, message := range req.ConversationMessages() {
		for partIndex, part := range message.Content.MultipleContent {
			typ := strings.ToLower(strings.TrimSpace(part.Type))
			if outboundType == OutboundTypeOpenAIChat && typ == "document" {
				reportLoss(decision, fmt.Sprintf("messages[%d].content[%d]", messageIndex, partIndex), LossActionTranslate,
					"OpenAI Chat converts document content to a plain-text hint")
				continue
			}
			if supportsContentPart(outboundType, typ) {
				continue
			}
			degrade(decision, fmt.Sprintf("messages[%d].content[%d]", messageIndex, partIndex), fmt.Sprintf("content part %q is not natively representable on %s", typ, outboundType))
		}
	}
	if len(req.Modalities) > 0 {
		for _, modality := range req.Modalities {
			if strings.EqualFold(modality, "text") {
				continue
			}
			if outboundType == OutboundTypeGemini {
				if !geminiOutbound.SupportsResponseModality(modality) {
					reportLoss(decision, "modalities", LossActionDrop,
						fmt.Sprintf("output modality %q is dropped because Gemini does not support it", modality))
				}
				continue
			}
			if outboundType == OutboundTypeAnthropic || outboundType == OutboundTypeOpenAIResponse {
				degrade(decision, "modalities", fmt.Sprintf("output modality %q is not supported by %s", modality, outboundType))
			}
		}
	}
}

func evaluateReasoning(req *model.InternalLLMRequest, outboundType OutboundType, decision *CapabilityDecision) {
	degradeIf := func(condition bool, field string) {
		if condition {
			degrade(decision, field, fmt.Sprintf("%s does not preserve %s", outboundType, field))
		}
	}
	usesSummary := req.ReasoningSummary != nil || req.ReasoningGenerateSummary != nil
	switch outboundType {
	case OutboundTypeOpenAIChat:
		degradeIf(req.ReasoningBudget != nil, "reasoning_budget")
		degradeIf(req.AdaptiveThinking, "adaptive_thinking")
		degradeIf(req.EnableThinking != nil, "enable_thinking")
		degradeIf(usesSummary, "reasoning_summary")
	case OutboundTypeOpenAIResponse:
		degradeIf(req.AdaptiveThinking, "adaptive_thinking")
		degradeIf(req.EnableThinking != nil, "enable_thinking")
		degradeIf(req.Thinking != nil, "thinking")
	case OutboundTypeAnthropic:
		degradeIf(req.ReasoningBudget != nil && req.ReasoningEffort == "", "reasoning_budget")
		degradeIf(req.EnableThinking != nil, "enable_thinking")
		degradeIf(req.Thinking != nil, "thinking")
		degradeIf(usesSummary, "reasoning_summary")
	case OutboundTypeGemini:
		degradeIf(req.EnableThinking != nil, "enable_thinking")
		degradeIf(req.Thinking != nil, "thinking")
		degradeIf(usesSummary, "reasoning_summary")
	}
}

func supportsContentPart(outboundType OutboundType, typ string) bool {
	switch typ {
	case "", "text", "image_url":
		return true
	case "input_audio":
		return outboundType == OutboundTypeOpenAIChat || outboundType == OutboundTypeOpenAIResponse || outboundType == OutboundTypeGemini
	case "file":
		return outboundType == OutboundTypeOpenAIResponse || outboundType == OutboundTypeGemini
	case "document":
		return outboundType == OutboundTypeAnthropic || outboundType == OutboundTypeGemini
	case "server_tool_use", "server_tool_result":
		return outboundType == OutboundTypeAnthropic
	default:
		return false
	}
}

func evaluateProviderSpecificSemantics(req *model.InternalLLMRequest, outboundType OutboundType, decision *CapabilityDecision) {
	if outboundType != OutboundTypeAnthropic {
		anthropic := req.GetAnthropicExtensions()
		if len(anthropic.MCPServers) > 0 {
			degrade(decision, "provider_extensions.anthropic.mcp_servers", "Anthropic MCP servers are dropped on non-Anthropic endpoints")
		}
		if len(anthropic.Container) > 0 {
			degrade(decision, "provider_extensions.anthropic.container", "Anthropic container state is dropped on non-Anthropic endpoints")
		}
	}
}

func hasStructuredOutput(format *model.ResponseFormat) bool {
	if format == nil {
		return false
	}
	return format.Type == "json_object" || format.Type == "json_schema" || format.Schema != nil || len(format.RawSchema) > 0 || len(format.JSONSchema) > 0
}

func hasMultimodalSemantics(req *model.InternalLLMRequest) bool {
	for _, modality := range req.Modalities {
		if !strings.EqualFold(modality, "text") {
			return true
		}
	}
	for _, message := range req.ConversationMessages() {
		for _, part := range message.Content.MultipleContent {
			if typ := strings.ToLower(strings.TrimSpace(part.Type)); typ != "" && typ != "text" {
				return true
			}
		}
	}
	return false
}
