package relay

import (
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func emptyResponseDetectionEnabled() bool {
	enabled, err := op.SettingGetBool(dbmodel.SettingKeyEmptyResponseDetectionEnabled)
	if err != nil {
		log.Warnf("failed to read empty response detection setting, defaulting to enabled: %v", err)
		return true
	}
	return enabled
}

// hasNonStreamContent checks if a non-streaming InternalLLMResponse contains meaningful content.
// Returns false for empty responses that should trigger retry.
func hasNonStreamContent(resp *model.InternalLLMResponse) bool {
	if resp == nil {
		return false
	}

	// Error responses are considered meaningful (handled separately)
	if resp.Error != nil {
		return true
	}

	// Check embedding responses
	if len(resp.EmbeddingData) > 0 {
		return true
	}

	// Check chat completion choices
	for _, choice := range resp.Choices {
		if choice.Message != nil && hasMessageContent(choice.Message) {
			return true
		}
	}

	return false
}

// hasMessageContent checks if a Message contains meaningful content.
func hasMessageContent(msg *model.Message) bool {
	if msg == nil {
		return false
	}

	// Text content
	if msg.Content.Content != nil && strings.TrimSpace(*msg.Content.Content) != "" {
		return true
	}

	// Multiple content parts (images, text blocks, etc.)
	if len(msg.Content.MultipleContent) > 0 {
		return true
	}

	// Tool calls
	if len(msg.ToolCalls) > 0 {
		return true
	}

	// Reasoning content (deepseek-reasoner, OpenRouter, etc.)
	if msg.ReasoningContent != nil && strings.TrimSpace(*msg.ReasoningContent) != "" {
		return true
	}
	if msg.Reasoning != nil && strings.TrimSpace(*msg.Reasoning) != "" {
		return true
	}

	// Reasoning blocks (Anthropic extended thinking, Gemini)
	if len(msg.ReasoningBlocks) > 0 {
		return true
	}

	// Refusal messages are considered content
	if strings.TrimSpace(msg.Refusal) != "" {
		return true
	}

	// Audio content
	if msg.Audio != nil {
		return true
	}

	// Image responses (some providers like Gemini)
	if len(msg.Images) > 0 {
		return true
	}

	return false
}

// validateNonStreamResponse checks if a non-streaming response has meaningful content
// and returns an error if it's empty, triggering the retry mechanism.
func validateNonStreamResponse(resp *model.InternalLLMResponse) error {
	if !hasNonStreamContent(resp) {
		return stream.ErrEmptyUpstreamStream
	}
	return nil
}

// allowEmptyPayload reports whether a metadata-only upstream stream should be
// flushed downstream as a success instead of failing over.
//
// The precommit buffer itself always runs — it is what keeps failover possible
// when an upstream emits headers and then dies — so this switch only suppresses
// the empty-response verdict. A stream that forwards nothing at all still fails
// with ErrEmptyUpstreamStream regardless.
func (ra *relayAttempt) allowEmptyPayload() bool {
	return ra == nil || !ra.emptyResponseDetection
}
