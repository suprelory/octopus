package model

import (
	"sort"
	"strings"
)

// StreamAggregator folds chunks as they arrive. It owns the accumulated data;
// callers may reuse input chunks and mutate Response snapshots independently.
// Like the stream it represents, an aggregator is used by one goroutine.
type StreamAggregator struct {
	response *InternalLLMResponse
	choices  map[int]*streamChoiceAggregate
}

func (a *StreamAggregator) Add(chunk *InternalLLMResponse) {
	if a == nil || chunk == nil || chunk.Object == "[DONE]" {
		return
	}
	if a.response == nil {
		a.response = &InternalLLMResponse{Object: "chat.completion"}
		a.choices = make(map[int]*streamChoiceAggregate)
	}
	result := a.response
	if chunk.ID != "" {
		result.ID = chunk.ID
	}
	if chunk.Model != "" {
		result.Model = chunk.Model
	}
	if chunk.Created != 0 {
		result.Created = chunk.Created
	}
	if chunk.SystemFingerprint != "" {
		result.SystemFingerprint = chunk.SystemFingerprint
	}
	if chunk.ServiceTier != "" {
		result.ServiceTier = chunk.ServiceTier
	}
	if chunk.Usage != nil {
		result.Usage = deepClone(chunk.Usage).(*Usage)
	}
	if chunk.Error != nil {
		result.Error = deepClone(chunk.Error).(*ResponseError)
	}
	if len(chunk.RawResponsesOutputItems) > 0 {
		result.RawResponsesOutputItems = append(result.RawResponsesOutputItems[:0], chunk.RawResponsesOutputItems...)
	}
	for _, event := range chunk.NonChatStreamEvents {
		result.NonChatStreamEvents = append(result.NonChatStreamEvents, cloneNonChatStreamEvent(event))
	}
	for _, choice := range chunk.Choices {
		state := a.choices[choice.Index]
		if state == nil {
			state = &streamChoiceAggregate{choice: Choice{Index: choice.Index, Message: &Message{}}}
			a.choices[choice.Index] = state
		}
		state.add(choice)
	}
}

func (a *StreamAggregator) Reset() {
	if a != nil {
		*a = StreamAggregator{}
	}
}

// Response returns an independent snapshot without replaying previous chunks.
func (a *StreamAggregator) Response() *InternalLLMResponse {
	response := a.build()
	if response == nil {
		return nil
	}
	return deepClone(response).(*InternalLLMResponse)
}

// BuildAndReset transfers the accumulated data to the caller without copying it.
func (a *StreamAggregator) BuildAndReset() *InternalLLMResponse {
	response := a.build()
	a.Reset()
	return response
}

func (a *StreamAggregator) build() *InternalLLMResponse {
	if a == nil || a.response == nil {
		return nil
	}
	result := *a.response
	indices := make([]int, 0, len(a.choices))
	for index := range a.choices {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	result.Choices = make([]Choice, 0, len(indices))
	for _, index := range indices {
		result.Choices = append(result.Choices, a.choices[index].build())
	}
	return &result
}

type streamChoiceAggregate struct {
	choice                Choice
	content               streamContentAggregate
	reasoning, refusal    strings.Builder
	audioData, transcript strings.Builder
	tools                 []*streamToolAggregate
	toolIndices           map[int]int
}

type streamToolAggregate struct {
	call      ToolCall
	arguments strings.Builder
}

func (s *streamChoiceAggregate) add(choice Choice) {
	existingChoice := &s.choice
	if choice.Grounding != nil {
		existingChoice.Grounding = deepClone(choice.Grounding).(*GroundingInfo)
	}
	if choice.URLContext != nil {
		existingChoice.URLContext = deepClone(choice.URLContext).(*URLContextInfo)
	}
	if choice.SafetyRatings != nil {
		existingChoice.SafetyRatings = append([]SafetyRating(nil), choice.SafetyRatings...)
	}
	if choice.Delta != nil {
		delta := choice.Delta
		if delta.Role != "" {
			existingChoice.Message.Role = delta.Role
		}
		s.content.add(delta.Content)
		for _, image := range delta.Images {
			s.content.appendPart(image)
		}
		if delta.Audio != nil {
			if existingChoice.Message.Audio == nil {
				existingChoice.Message.Audio = &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{}
			}
			if delta.Audio.ID != "" {
				existingChoice.Message.Audio.ID = delta.Audio.ID
			}
			if delta.Audio.ExpiresAt > 0 {
				existingChoice.Message.Audio.ExpiresAt = delta.Audio.ExpiresAt
			}
			s.audioData.WriteString(delta.Audio.Data)
			s.transcript.WriteString(delta.Audio.Transcript)
		}
		if reasoning := delta.GetReasoningContent(); reasoning != "" {
			s.reasoning.WriteString(reasoning)
		}
		if delta.ReasoningSignatureSource != nil {
			existingChoice.Message.SetOpaqueReasoningSignature(*delta.ReasoningSignatureSource)
		} else if delta.ReasoningSignature != nil && *delta.ReasoningSignature != "" {
			signature := *delta.ReasoningSignature
			existingChoice.Message.ReasoningSignature = &signature
		}
		if len(delta.ReasoningBlocks) > 0 {
			existingChoice.Message.ReasoningBlocks = append(existingChoice.Message.ReasoningBlocks, deepClone(delta.ReasoningBlocks).([]ReasoningBlock)...)
		}
		if len(delta.RedactedThinkingBlocks) > 0 {
			existingChoice.Message.RedactedThinkingBlocks = append(existingChoice.Message.RedactedThinkingBlocks, delta.RedactedThinkingBlocks...)
		}
		for _, toolCall := range delta.ToolCalls {
			s.addTool(toolCall)
		}
		if delta.Refusal != "" {
			s.refusal.WriteString(delta.Refusal)
		}
	}
	if len(choice.Citations) > 0 {
		existingChoice.Citations = append(existingChoice.Citations, cloneCitations(choice.Citations)...)
	}
	if choice.FinishReason != nil {
		existingChoice.FinishReason = cloneStringPtr(choice.FinishReason)
	}
	if choice.StopSequence != nil {
		existingChoice.StopSequence = cloneStringPtr(choice.StopSequence)
	}
	if choice.Logprobs != nil {
		if existingChoice.Logprobs == nil {
			existingChoice.Logprobs = &LogprobsContent{}
		}
		existingChoice.Logprobs.Content = append(existingChoice.Logprobs.Content, deepClone(choice.Logprobs.Content).([]TokenLogprob)...)
	}
}

func (s *streamChoiceAggregate) addTool(delta ToolCall) {
	if s.toolIndices == nil {
		s.toolIndices = make(map[int]int)
	}
	index, exists := s.toolIndices[delta.Index]
	if !exists {
		index = len(s.tools)
		s.toolIndices[delta.Index] = index
		s.tools = append(s.tools, &streamToolAggregate{call: ToolCall{Index: delta.Index}})
	}
	tool := s.tools[index]
	tool.arguments.WriteString(delta.Function.Arguments)
	delta.Function.Arguments = ""
	delta.ProviderExtensions = CloneProviderExtensions(delta.ProviderExtensions)
	delta.CacheControl = cloneCacheControl(delta.CacheControl)
	merged := MergeToolCallDelta([]ToolCall{tool.call}, delta)
	tool.call = merged[0]
}

func (s *streamChoiceAggregate) build() Choice {
	choice := s.choice
	message := *choice.Message
	choice.Message = &message
	message.Content = s.content.build()
	if s.reasoning.Len() > 0 {
		reasoning := s.reasoning.String()
		message.ReasoningContent = &reasoning
	}
	message.Refusal = s.refusal.String()
	if message.Audio != nil {
		audio := *message.Audio
		audio.Data, audio.Transcript = s.audioData.String(), s.transcript.String()
		message.Audio = &audio
	}
	if len(s.tools) > 0 {
		message.ToolCalls = make([]ToolCall, len(s.tools))
		for index, tool := range s.tools {
			message.ToolCalls[index] = tool.call
			message.ToolCalls[index].Function.Arguments = tool.arguments.String()
		}
	}
	return choice
}

func MergeToolCallDelta(toolCalls []ToolCall, delta ToolCall) []ToolCall {
	for i, tc := range toolCalls {
		if tc.Index == delta.Index {
			if delta.ID != "" {
				toolCalls[i].ID = delta.ID
			}
			if delta.Type != "" {
				toolCalls[i].Type = delta.Type
			}
			if delta.Function.Name != "" {
				if toolCalls[i].Function.Name == "" {
					toolCalls[i].Function.Name = delta.Function.Name
				} else if toolCalls[i].Function.Name != delta.Function.Name {
					toolCalls[i].Function.Name += delta.Function.Name
				}
			}
			if delta.Function.Arguments != "" {
				toolCalls[i].Function.Arguments += delta.Function.Arguments
			}
			if delta.Function.Namespace != "" {
				toolCalls[i].Function.Namespace = delta.Function.Namespace
			}
			if delta.ThoughtSignature != "" {
				toolCalls[i].ThoughtSignature = delta.ThoughtSignature
			}
			if delta.ProviderExtensions != nil {
				toolCalls[i].ProviderExtensions = delta.ProviderExtensions
			}
			if delta.CacheControl != nil {
				toolCalls[i].CacheControl = delta.CacheControl
			}
			return toolCalls
		}
	}
	return append(toolCalls, delta)
}
