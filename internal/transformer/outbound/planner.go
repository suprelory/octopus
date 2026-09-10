package outbound

import (
	"encoding/json"
	"fmt"
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
	Losses           LossReport
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

// PlanRequestForModel evaluates protocol-level semantic compatibility before a
// key is selected or request bytes are sent upstream. The planner treats req as
// read-only; effectiveModel is used for provider-family checks whose result
// depends on the selected upstream model.
func PlanRequestForModel(req *model.InternalLLMRequest, effectiveModel string, outboundType OutboundType, passthrough bool) CapabilityDecision {
	decision := CapabilityDecision{Status: CapabilitySupported, Lossiness: "none"}
	if req == nil {
		return rejectDecision(decision, "request is nil")
	}
	if err := req.ValidateOperationConsistency(); err != nil {
		return rejectDecision(decision, err.Error())
	}

	decision.RequestType = req.ResolveRequestType()
	decision.InboundFormat = req.RawAPIFormat
	capability, ok := Descriptor(outboundType)
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
	if req.HasOpenAIResponsesPassthrough() && !decision.Passthrough && !supportsOpenAIResponsesRecovery(req, outboundType) {
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
		evaluateFeature(req, effectiveModel, outboundType, SemanticFeature(feature), &decision)
	}
	evaluateAdapterFieldLosses(req, outboundType, &decision)
	evaluateAdapterReportedChanges(req, effectiveModel, outboundType, &decision)
	evaluateProviderSpecificSemantics(req, outboundType, &decision)
	evaluateInboundRepairs(req, &decision)
	decision.DegradedFields = uniqueSorted(decision.DegradedFields)
	decision.Reasons = uniqueSorted(decision.Reasons)
	decision.Losses = uniqueLossReports(decision.Losses)
	if len(decision.DegradedFields) > 0 {
		decision.Status = CapabilityDegraded
		decision.Lossiness = "known"
	}
	return decision
}

// supportsOpenAIResponsesRecovery identifies the two Responses paths that
// intentionally use the canonical builder instead of raw passthrough. The
// exception is fail-closed: every native-only reason must have its raw sidecar
// available so recovery cannot silently drop an input item or tool definition.
func supportsOpenAIResponsesRecovery(req *model.InternalLLMRequest, outboundType OutboundType) bool {
	if req == nil || outboundType != OutboundTypeOpenAIResponse {
		return false
	}
	if !req.IsOpenAIExactReplayRequest() && req.OpenAIPreviousResponseID() == "" {
		return false
	}

	reasons := strings.Split(req.OpenAIResponsesPassthroughReasonTextValue(), ",")
	if len(reasons) == 0 {
		return false
	}
	responsesOptions := req.GetOpenAIResponsesOptions()
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		switch {
		case strings.HasPrefix(reason, "input:"):
			if !hasRawJSONArray(req.OpenAIRawInputItems()) {
				return false
			}
		case strings.HasPrefix(reason, "tool:"):
			if !hasRawJSONArray(responsesOptions.RawTools) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func hasRawJSONArray(raw json.RawMessage) bool {
	var values []json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &values) == nil && len(values) > 0
}

// PlanRelayOperation evaluates auxiliary endpoints that are proxied without
// passing through InternalLLMRequest. These decisions still use the relay-wide
// capability policy and audit trace, while field-level planning remains the
// responsibility of PlanRequestForModel.
func PlanRelayOperation(outboundType OutboundType, operation string) CapabilityDecision {
	decision := CapabilityDecision{
		Status:        CapabilitySupported,
		Lossiness:     "none",
		StaticQuality: QualityNative,
	}
	operation = strings.TrimSpace(operation)
	if operation == "" {
		decision.StaticQuality = QualityUnsupported
		return rejectDecision(decision, "relay operation is empty")
	}
	decision.RequiredFeatures = []string{"relay_operation:" + operation}
	decision.RequestType, decision.InboundFormat = relayOperationRequest(operation)

	descriptor, ok := Descriptor(outboundType)
	if !ok {
		decision.StaticQuality = QualityUnsupported
		return rejectDecision(decision, fmt.Sprintf("unsupported outbound type %d", outboundType))
	}
	decision.OutboundFormat = descriptor.APIFormat
	decision.ConversionPath = []string{operation, descriptor.Name}
	if !descriptor.SupportsRelayOperation(operation) {
		decision.StaticQuality = QualityUnsupported
		return rejectDecision(decision, fmt.Sprintf("channel does not support relay operation %s", operation))
	}
	return decision
}

func relayOperationRequest(operation string) (model.RequestType, model.APIFormat) {
	switch operation {
	case RelayOperationImages:
		return model.RequestTypeImages, model.APIFormatOpenAIImageGeneration
	case RelayOperationResponsesCompact, RelayOperationResponsesWebSocket:
		return model.RequestTypeResponses, model.APIFormatOpenAIResponse
	default:
		return "", ""
	}
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

func uniqueLossReports(reports LossReport) LossReport {
	if len(reports) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(reports))
	result := make(LossReport, 0, len(reports))
	for _, report := range reports {
		report.Field = strings.TrimSpace(report.Field)
		report.Reason = strings.TrimSpace(report.Reason)
		if report.Field == "" || report.Action == "" {
			continue
		}
		key := string(report.Action) + "\x00" + report.Field + "\x00" + report.Reason
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, report)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Field != result[j].Field {
			return result[i].Field < result[j].Field
		}
		if result[i].Action != result[j].Action {
			return result[i].Action < result[j].Action
		}
		return result[i].Reason < result[j].Reason
	})
	return result
}
