package model

import "encoding/json"

type StreamMessageMetadata struct {
	Status            string                `json:"status,omitempty"`
	ProviderMetadata  json.RawMessage       `json:"provider_metadata,omitempty"`
	Created           int64                 `json:"created,omitempty"`
	SystemFingerprint string                `json:"system_fingerprint,omitempty"`
	ServiceTier       string                `json:"service_tier,omitempty"`
	Choice            *StreamChoiceMetadata `json:"choice,omitempty"`
}

type StreamChoiceMetadata struct {
	Logprobs      *LogprobsContent `json:"logprobs,omitempty"`
	Grounding     *GroundingInfo   `json:"grounding,omitempty"`
	URLContext    *URLContextInfo  `json:"url_context,omitempty"`
	SafetyRatings []SafetyRating   `json:"safety_ratings,omitempty"`
}

func responseMetadataEvent(response *InternalLLMResponse) []StreamEvent {
	if response.Created == 0 && response.SystemFingerprint == "" && response.ServiceTier == "" {
		return nil
	}
	return []StreamEvent{{Kind: StreamEventKindMessageMetadata, ID: response.ID, Model: response.Model, Metadata: &StreamMessageMetadata{Created: response.Created, SystemFingerprint: response.SystemFingerprint, ServiceTier: response.ServiceTier}}}
}

func choiceMetadataEvent(choice Choice, response *InternalLLMResponse) []StreamEvent {
	if choice.Logprobs == nil && choice.Grounding == nil && choice.URLContext == nil && len(choice.SafetyRatings) == 0 {
		return nil
	}
	return []StreamEvent{{Kind: StreamEventKindMessageMetadata, ID: response.ID, Model: response.Model, Index: choice.Index, Metadata: &StreamMessageMetadata{Choice: &StreamChoiceMetadata{Logprobs: choice.Logprobs, Grounding: choice.Grounding, URLContext: choice.URLContext, SafetyRatings: choice.SafetyRatings}}}}
}
