package outbound

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type CapabilityStatus string

const (
	CapabilitySupported CapabilityStatus = "supported"
	CapabilityDegraded  CapabilityStatus = "degraded"
	CapabilityRejected  CapabilityStatus = "rejected"
)

type SemanticFeature string

const (
	FeatureTools            SemanticFeature = "tools"
	FeatureToolChoice       SemanticFeature = "tool_choice"
	FeatureReasoning        SemanticFeature = "reasoning"
	FeatureStructuredOutput SemanticFeature = "structured_output"
	FeatureMultimodal       SemanticFeature = "multimodal"
	FeatureStreamUsage      SemanticFeature = "stream_usage"
)

type CapabilityDecision struct {
	Status           CapabilityStatus
	RequestType      model.RequestType
	InboundFormat    model.APIFormat
	OutboundFormat   model.APIFormat
	ConversionPath   []string
	RequiredFeatures []string
	DegradedFields   []string
	Reasons          []string
	Lossiness        string
	StaticQuality    ConversionQuality
	Passthrough      bool
}

func (d CapabilityDecision) Rejected() bool {
	return d.Status == CapabilityRejected
}

func (d CapabilityDecision) Summary() string {
	if len(d.Reasons) > 0 {
		return strings.Join(d.Reasons, "; ")
	}
	return string(d.Status)
}

// PlanRequest evaluates protocol-level semantic compatibility before a key is
// selected or request bytes are sent upstream. Model-family capability remains
// the provider's responsibility; this planner covers transformations Octopus
// itself can prove to be native, lossy, or impossible.
func PlanRequest(req *model.InternalLLMRequest, outboundType OutboundType, passthrough bool) CapabilityDecision {
	decision := CapabilityDecision{Status: CapabilitySupported, Lossiness: "none"}
	if req == nil {
		return rejectDecision(decision, "request is nil")
	}

	decision.RequestType = req.ResolveRequestType()
	decision.InboundFormat = req.RawAPIFormat
	capability, ok := Capabilities(outboundType)
	if !ok {
		return rejectDecision(decision, fmt.Sprintf("unsupported outbound type %d", outboundType))
	}
	decision.OutboundFormat = capability.APIFormat
	decision.StaticQuality = StaticConversionQuality(req.RawAPIFormat, outboundType, decision.RequestType)
	decision.Passthrough = passthrough && SupportsNativeFormat(outboundType, req.RawAPIFormat)
	decision.ConversionPath = conversionPath(req.RawAPIFormat, capability.APIFormat, decision.Passthrough)
	decision.RequiredFeatures = requestedFeatures(req)

	if !capability.Supports(decision.RequestType) {
		return rejectDecision(decision, fmt.Sprintf("channel does not support %s requests", decision.RequestType))
	}
	if req.HasOpenAIResponsesPassthrough() && !SupportsNativeFormat(outboundType, model.APIFormatOpenAIResponse) {
		reason := "该请求包含仅支持 OpenAI Responses 通道直通的原生语义"
		if detail := req.OpenAIResponsesPassthroughReasonTextValue(); detail != "" {
			reason += ": " + detail
		}
		return rejectDecision(decision, reason)
	}
	if decision.Passthrough {
		return decision
	}

	for _, feature := range decision.RequiredFeatures {
		evaluateFeature(req, outboundType, SemanticFeature(feature), &decision)
	}
	evaluateProviderSpecificSemantics(req, outboundType, &decision)
	decision.DegradedFields = uniqueSorted(decision.DegradedFields)
	decision.Reasons = uniqueSorted(decision.Reasons)
	if len(decision.DegradedFields) > 0 {
		decision.Status = CapabilityDegraded
		decision.Lossiness = "known"
	}
	return decision
}

func rejectDecision(decision CapabilityDecision, reason string) CapabilityDecision {
	decision.Status = CapabilityRejected
	decision.Lossiness = "rejected"
	decision.Reasons = append(decision.Reasons, reason)
	return decision
}

func conversionPath(inboundFormat, outboundFormat model.APIFormat, passthrough bool) []string {
	inbound := string(inboundFormat)
	if inbound == "" {
		inbound = "unknown"
	}
	if passthrough {
		return []string{inbound, "raw_passthrough", string(outboundFormat)}
	}
	return []string{inbound, "canonical", string(outboundFormat)}
}

func requestedFeatures(req *model.InternalLLMRequest) []string {
	features := make([]string, 0, 6)
	if len(req.Tools) > 0 {
		features = append(features, string(FeatureTools))
	}
	if req.ToolChoice != nil {
		features = append(features, string(FeatureToolChoice))
	}
	if req.ReasoningEffort != "" || req.ReasoningBudget != nil || req.AdaptiveThinking || req.EnableThinking != nil || req.Thinking != nil || req.ReasoningSummary != nil || req.ReasoningGenerateSummary != nil {
		features = append(features, string(FeatureReasoning))
	}
	if hasStructuredOutput(req.ResponseFormat) {
		features = append(features, string(FeatureStructuredOutput))
	}
	if hasMultimodalSemantics(req) {
		features = append(features, string(FeatureMultimodal))
	}
	if req.Stream != nil && *req.Stream && (req.StreamOptions == nil || req.StreamOptions.IncludeUsage || req.RawAPIFormat != model.APIFormatOpenAIChatCompletion) {
		features = append(features, string(FeatureStreamUsage))
	}
	return features
}

func evaluateFeature(req *model.InternalLLMRequest, outboundType OutboundType, feature SemanticFeature, decision *CapabilityDecision) {
	switch feature {
	case FeatureStructuredOutput:
		switch outboundType {
		case OutboundTypeAnthropic:
			degrade(decision, "response_format", "Anthropic transformer cannot emit the canonical response_format body")
		case OutboundTypeGemini:
			if req.ResponseFormat != nil && req.ResponseFormat.Schema != nil {
				if _, err := req.ResponseFormat.Schema.ToGemini(); errors.Is(err, model.ErrSchemaLossy) {
					degrade(decision, "response_format.schema", err.Error())
				}
			}
		}
	case FeatureToolChoice:
		if req.ToolChoice != nil && req.ToolChoice.NamedToolChoice != nil && req.ToolChoice.NamedToolChoice.DisableParallelToolUse != nil && outboundType != OutboundTypeAnthropic {
			degrade(decision, "tool_choice.disable_parallel_tool_use", "target protocol has no equivalent disable_parallel_tool_use control")
		}
	case FeatureReasoning:
		evaluateReasoning(req, outboundType, decision)
		if outboundType == OutboundTypeVolcengine {
			if _, ok := supportedVolcengineReasoningModels[req.Model]; !ok {
				degrade(decision, "reasoning", "Volcengine endpoint drops reasoning configuration for this model")
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

var supportedVolcengineReasoningModels = map[string]struct{}{
	"doubao-seed-1-8-251228":      {},
	"doubao-seed-1-6-lite-251015": {},
	"doubao-seed-1-6-251015":      {},
}

func supportsToolType(outboundType OutboundType, tool model.Tool) bool {
	typ := strings.ToLower(strings.TrimSpace(tool.Type))
	if typ == "" || typ == "function" {
		return true
	}
	switch outboundType {
	case OutboundTypeOpenAIResponse, OutboundTypeVolcengine:
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
	for messageIndex, message := range req.Messages {
		for partIndex, part := range message.Content.MultipleContent {
			typ := strings.ToLower(strings.TrimSpace(part.Type))
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
			if outboundType == OutboundTypeAnthropic || outboundType == OutboundTypeOpenAIResponse || outboundType == OutboundTypeVolcengine {
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
	case OutboundTypeOpenAIResponse, OutboundTypeVolcengine:
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
		return outboundType == OutboundTypeOpenAIChat || outboundType == OutboundTypeOpenAIResponse || outboundType == OutboundTypeVolcengine || outboundType == OutboundTypeGemini
	case "file":
		return outboundType == OutboundTypeOpenAIResponse || outboundType == OutboundTypeVolcengine || outboundType == OutboundTypeGemini
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

func degrade(decision *CapabilityDecision, field, reason string) {
	decision.DegradedFields = append(decision.DegradedFields, field)
	decision.Reasons = append(decision.Reasons, reason)
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
	for _, message := range req.Messages {
		for _, part := range message.Content.MultipleContent {
			if typ := strings.ToLower(strings.TrimSpace(part.Type)); typ != "" && typ != "text" {
				return true
			}
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
