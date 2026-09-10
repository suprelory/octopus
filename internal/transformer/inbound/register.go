package inbound

import (
	"github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/inbound/openai"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"sort"
)

type InboundType int

const (
	InboundTypeOpenAIChat InboundType = iota
	InboundTypeOpenAIResponse
	InboundTypeAnthropic
	InboundTypeOpenAIEmbedding
)

var inboundFactories = map[InboundType]func() model.Inbound{
	InboundTypeOpenAIChat:      func() model.Inbound { return &openai.ChatInbound{} },
	InboundTypeOpenAIResponse:  func() model.Inbound { return &openai.ResponseInbound{} },
	InboundTypeOpenAIEmbedding: func() model.Inbound { return &openai.EmbeddingInbound{} },
	InboundTypeAnthropic:       func() model.Inbound { return &anthropic.MessagesInbound{} },
}

func Get(inboundType InboundType) model.Inbound {
	if factory, ok := inboundFactories[inboundType]; ok {
		return factory()
	}
	return nil
}

func Types() []InboundType {
	types := make([]InboundType, 0, len(inboundFactories))
	for typ := range inboundFactories {
		types = append(types, typ)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}
