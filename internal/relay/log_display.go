package relay

import "github.com/bestruirui/octopus/internal/transformer/inbound"

func relayEndpointType(inboundType inbound.InboundType) string {
	switch inboundType {
	case inbound.InboundTypeOpenAIResponse:
		return "responses"
	case inbound.InboundTypeAnthropic:
		return "messages"
	case inbound.InboundTypeOpenAIEmbedding:
		return "embeddings"
	default:
		return "chat"
	}
}
