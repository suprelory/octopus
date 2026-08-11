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

// EndpointCapability describes the semantic operations and wire format
// exposed by one outbound endpoint. Keeping this next to the factory registry
// prevents routing checks from drifting away from transformer registration.
type EndpointCapability struct {
	APIFormat          model.APIFormat
	RequestTypes       map[model.RequestType]struct{}
	NativeInputFormats map[model.APIFormat]struct{}
}

func (c EndpointCapability) Supports(requestType model.RequestType) bool {
	_, ok := c.RequestTypes[requestType]
	return ok
}

var endpointCapabilities = map[OutboundType]EndpointCapability{
	OutboundTypeOpenAIChat: {
		APIFormat:    model.APIFormatOpenAIChatCompletion,
		RequestTypes: requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
	},
	OutboundTypeOpenAIResponse: {
		APIFormat:          model.APIFormatOpenAIResponse,
		RequestTypes:       requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		NativeInputFormats: apiFormats(model.APIFormatOpenAIResponse),
	},
	OutboundTypeAnthropic: {
		APIFormat:          model.APIFormatAnthropicMessage,
		RequestTypes:       requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
		NativeInputFormats: apiFormats(model.APIFormatAnthropicMessage),
	},
	OutboundTypeGemini: {
		APIFormat:    model.APIFormatGeminiContents,
		RequestTypes: requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
	},
	OutboundTypeVolcengine: {
		APIFormat:    model.APIFormatOpenAIResponse,
		RequestTypes: requestTypes(model.RequestTypeChat, model.RequestTypeResponses),
	},
	OutboundTypeOpenAIEmbedding: {
		APIFormat:    model.APIFormatOpenAIEmbedding,
		RequestTypes: requestTypes(model.RequestTypeEmbedding),
	},
}

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

func Capabilities(outboundType OutboundType) (EndpointCapability, bool) {
	capability, ok := endpointCapabilities[outboundType]
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

var outboundFactories = map[OutboundType]func() model.Outbound{
	OutboundTypeOpenAIChat:      func() model.Outbound { return &openai.ChatOutbound{} },
	OutboundTypeOpenAIResponse:  func() model.Outbound { return &openai.ResponseOutbound{} },
	OutboundTypeOpenAIEmbedding: func() model.Outbound { return &openai.EmbeddingOutbound{} },
	OutboundTypeAnthropic:       func() model.Outbound { return &outAnthropic.MessageOutbound{} },
	OutboundTypeGemini:          func() model.Outbound { return &gemini.MessagesOutbound{} },
	OutboundTypeVolcengine:      func() model.Outbound { return &volcengine.ResponseOutbound{} },
}

func Get(outboundType OutboundType) model.Outbound {
	if factory, ok := outboundFactories[outboundType]; ok {
		return factory()
	}
	return nil
}
