package outbound

import (
	"github.com/bestruirui/octopus/internal/transformer/model"
	outAnthropic "github.com/bestruirui/octopus/internal/transformer/outbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/outbound/gemini"
	"github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/transformer/outbound/volcengine"
)

type OutboundType int

const (
	OutboundTypeOpenAIChat OutboundType = iota
	OutboundTypeOpenAIResponse
	OutboundTypeAnthropic
	OutboundTypeGemini
	OutboundTypeVolcengine
	OutboundTypeOpenAIEmbedding
)

const (
	RelayOperationImages             = "images"
	RelayOperationResponsesCompact   = "responses/compact"
	RelayOperationResponsesWebSocket = "responses/websocket"
)

// ProtocolDescriptor is the single registry entry for an outbound protocol.
// It combines wire format, semantic operations, native passthrough support,
// relay-only auxiliary endpoints, transport metadata, and the transformer
// factory so those declarations cannot drift across separate maps.
type ProtocolDescriptor struct {
	Name               string
	APIFormat          model.APIFormat
	RequestTypes       map[model.RequestType]struct{}
	NativeInputFormats map[model.APIFormat]struct{}
	RelayOperations    map[string]struct{}
	Transport          string
	Factory            func() model.Outbound
}

// EndpointCapability remains as a source-compatible alias for callers that
// used the pre-descriptor registry name.
type EndpointCapability = ProtocolDescriptor

func (c ProtocolDescriptor) Supports(requestType model.RequestType) bool {
	_, ok := c.RequestTypes[requestType]
	return ok
}

func (c ProtocolDescriptor) SupportsRelayOperation(operation string) bool {
	_, ok := c.RelayOperations[operation]
	return ok
}

var protocolDescriptors = map[OutboundType]ProtocolDescriptor{
	OutboundTypeOpenAIChat: {
		Name:            "openai_chat",
		APIFormat:       model.APIFormatOpenAIChatCompletion,
		RequestTypes:    requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		RelayOperations: relayOperations(RelayOperationImages),
		Transport:       "http",
		Factory:         func() model.Outbound { return &openai.ChatOutbound{} },
	},
	OutboundTypeOpenAIResponse: {
		Name:               "openai_responses",
		APIFormat:          model.APIFormatOpenAIResponse,
		RequestTypes:       requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		NativeInputFormats: apiFormats(model.APIFormatOpenAIResponse),
		RelayOperations: relayOperations(
			RelayOperationImages,
			RelayOperationResponsesCompact,
			RelayOperationResponsesWebSocket,
		),
		Transport: "http+websocket",
		Factory:   func() model.Outbound { return &openai.ResponseOutbound{} },
	},
	OutboundTypeAnthropic: {
		Name:               "anthropic_messages",
		APIFormat:          model.APIFormatAnthropicMessage,
		RequestTypes:       requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		NativeInputFormats: apiFormats(model.APIFormatAnthropicMessage),
		Transport:          "http",
		Factory:            func() model.Outbound { return &outAnthropic.MessageOutbound{} },
	},
	OutboundTypeGemini: {
		Name:         "gemini_contents",
		APIFormat:    model.APIFormatGeminiContents,
		RequestTypes: requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		Transport:    "http",
		Factory:      func() model.Outbound { return &gemini.MessagesOutbound{} },
	},
	OutboundTypeVolcengine: {
		Name:         "volcengine_responses",
		APIFormat:    model.APIFormatOpenAIResponse,
		RequestTypes: requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		Transport:    "http",
		Factory:      func() model.Outbound { return &volcengine.ResponseOutbound{} },
	},
	OutboundTypeOpenAIEmbedding: {
		Name:         "openai_embeddings",
		APIFormat:    model.APIFormatOpenAIEmbedding,
		RequestTypes: requestTypes(model.RequestTypeEmbedding),
		Transport:    "http",
		Factory:      func() model.Outbound { return &openai.EmbeddingOutbound{} },
	},
}

// endpointCapabilities is retained for package-local quality matrix callers.
var endpointCapabilities = protocolDescriptors

func requestTypes(types ...model.RequestType) map[model.RequestType]struct{} {
	result := make(map[model.RequestType]struct{}, len(types))
	for _, requestType := range types {
		result[requestType] = struct{}{}
	}
	return result
}

func apiFormats(formats ...model.APIFormat) map[model.APIFormat]struct{} {
	result := make(map[model.APIFormat]struct{}, len(formats))
	for _, format := range formats {
		result[format] = struct{}{}
	}
	return result
}

func relayOperations(operations ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		result[operation] = struct{}{}
	}
	return result
}

func Descriptor(outboundType OutboundType) (ProtocolDescriptor, bool) {
	descriptor, ok := protocolDescriptors[outboundType]
	return descriptor, ok
}

func Capabilities(outboundType OutboundType) (EndpointCapability, bool) {
	capability, ok := Descriptor(outboundType)
	return capability, ok
}

func SupportsRequestType(outboundType OutboundType, requestType model.RequestType) bool {
	capability, ok := Capabilities(outboundType)
	return ok && capability.Supports(requestType)
}

func SupportsAPIFormat(outboundType OutboundType, format model.APIFormat) bool {
	capability, ok := Capabilities(outboundType)
	return ok && capability.APIFormat == format
}

func SupportsNativeFormat(outboundType OutboundType, format model.APIFormat) bool {
	capability, ok := Capabilities(outboundType)
	if !ok {
		return false
	}
	_, ok = capability.NativeInputFormats[format]
	return ok
}

func SupportsRelayOperation(outboundType OutboundType, operation string) bool {
	descriptor, ok := Descriptor(outboundType)
	return ok && descriptor.SupportsRelayOperation(operation)
}

func (t OutboundType) String() string {
	switch t {
	case OutboundTypeOpenAIChat:
		return "chat"
	case OutboundTypeOpenAIResponse:
		return "response"
	case OutboundTypeAnthropic:
		return "anthropic"
	case OutboundTypeGemini:
		return "gemini"
	case OutboundTypeVolcengine:
		return "volcengine"
	case OutboundTypeOpenAIEmbedding:
		return "embedding"
	default:
		return "unknown"
	}
}

// IsEmbeddingChannelType is kept for existing callers; the capability
// registry is the single source of truth.
func IsEmbeddingChannelType(channelType OutboundType) bool {
	return SupportsRequestType(channelType, model.RequestTypeEmbedding)
}

// IsChatChannelType is kept for existing callers; the capability registry is
// the single source of truth.
func IsChatChannelType(channelType OutboundType) bool {
	return SupportsRequestType(channelType, model.RequestTypeChat)
}

func Get(outboundType OutboundType) model.Outbound {
	if descriptor, ok := Descriptor(outboundType); ok && descriptor.Factory != nil {
		return descriptor.Factory()
	}
	return nil
}
