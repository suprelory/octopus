package outbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound/anthropic"
	geminiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/gemini"
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

// PlanRequest evaluates protocol-level semantic compatibility before a key is
// selected or request bytes are sent upstream. Model-family capability remains
// the provider's responsibility; this planner covers transformations Octopus
// itself can prove to be native, lossy, or impossible.
//
// The legacy entry point plans against req.Model. Callers that route one
// canonical request to multiple upstream model names should use
// PlanRequestForModel so they do not need to deep-copy the request merely to
// replace that one field.
func PlanRequest(req *model.InternalLLMRequest, outboundType OutboundType, passthrough bool) CapabilityDecision {
	if req == nil {
		return PlanRequestForModel(nil, "", outboundType, passthrough)
	}
	return PlanRequestForModel(req, req.Model, outboundType, passthrough)
}

// PlanRequestForModel is the model-parameterized form of PlanRequest. The
// planner treats req as read-only; effectiveModel is used for the small set of
// provider-family checks whose result depends on the selected upstream model.
func PlanRequestForModel(req *model.InternalLLMRequest, effectiveModel string, outboundType OutboundType, passthrough bool) CapabilityDecision {
	decision := CapabilityDecision{Status: CapabilitySupported, Lossiness: "none"}
	if req == nil {
		return rejectDecision(decision, "request is nil")
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
// responsibility of PlanRequest.
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
	for messageIndex, message := range req.Messages {
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
