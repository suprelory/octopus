package model

// Usage Represents the total token usage per request to OpenAI.
// For embedding requests, CompletionTokens is always 0.
//
// Cache semantics (detection via HasAnthropicCacheSemantic):
//   - Anthropic path: PromptTokens excludes cached tokens; CacheReadInputTokens /
//     CacheCreationInputTokens carry the split. PromptTokensDetails.CachedTokens is
//     mirrored for OpenAI-style downstreams.
//   - OpenAI / Gemini path: PromptTokens already includes cached reads (OpenAI
//     convention). CacheReadInputTokens stays zero; PromptTokensDetails.CachedTokens
//     holds the cached subset.
//
// Use the Billable* and EffectiveInputTokens helpers instead of branching on the
// upstream provider directly.
type Usage struct {
	PromptTokens            int64                    `json:"prompt_tokens"`
	CompletionTokens        int64                    `json:"completion_tokens"`
	TotalTokens             int64                    `json:"total_tokens"`
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details"`

	// Output only. A detailed breakdown of the token count for each modality in the prompt.
	PromptModalityTokenDetails []ModalityTokenCount `json:"-"`
	// Output only. A detailed breakdown of the token count for each modality in the candidates.
	CompletionModalityTokenDetails []ModalityTokenCount `json:"-"`

	// ToolUsePromptTokens is the portion of PromptTokens consumed by tool-use
	// prompts in multi-turn function calling (Gemini-specific). Included in
	// PromptTokens, surfaced separately for observability.
	ToolUsePromptTokens int64 `json:"tool_use_prompt_tokens,omitempty"`

	// Anthropic-style cache accounting. Presence of either CacheCreationInputTokens or
	// CacheReadInputTokens implies PromptTokens excludes cached tokens.
	CacheCreationInputTokens   int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheCreation5mInputTokens int64 `json:"cache_creation_5m_input_tokens,omitempty"`
	CacheCreation1hInputTokens int64 `json:"cache_creation_1h_input_tokens,omitempty"`
	CacheReadInputTokens       int64 `json:"cache_read_input_tokens,omitempty"`
}

func (u *Usage) GetCompletionTokens() *int64 {
	if u == nil {
		return nil
	}

	return &u.CompletionTokens
}

func (u *Usage) GetPromptTokens() *int64 {
	if u == nil {
		return nil
	}

	return &u.PromptTokens
}

// HasAnthropicCacheSemantic reports whether PromptTokens excludes cached tokens
// (the Anthropic Messages convention). When true, total input billing must add
// cache read/write on top of PromptTokens.
func (u *Usage) HasAnthropicCacheSemantic() bool {
	if u == nil {
		return false
	}
	return u.CacheCreationInputTokens > 0 || u.CacheReadInputTokens > 0
}

// BillableCacheReadInput returns the cache-read token count, preferring the
// explicit CacheReadInputTokens field and falling back to the OpenAI-style
// PromptTokensDetails.CachedTokens mirror.
func (u *Usage) BillableCacheReadInput() int64 {
	if u == nil {
		return 0
	}
	if u.CacheReadInputTokens > 0 {
		return u.CacheReadInputTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}

// BillableCacheWriteInput returns cache creation tokens (only populated on the
// Anthropic path).
func (u *Usage) BillableCacheWriteInput() int64 {
	if u == nil {
		return 0
	}
	return u.CacheCreationInputTokens
}

// BillableNonCachedInput returns the non-cached portion of the prompt used for
// standard input pricing. Semantics:
//   - Anthropic: PromptTokens already excludes cache, return as-is.
//   - OpenAI / Gemini: subtract cached tokens tracked in PromptTokensDetails.
func (u *Usage) BillableNonCachedInput() int64 {
	if u == nil {
		return 0
	}
	if u.HasAnthropicCacheSemantic() {
		if u.PromptTokens < 0 {
			return 0
		}
		return u.PromptTokens
	}
	cached := u.BillableCacheReadInput()
	n := u.PromptTokens - cached
	if n < 0 {
		return 0
	}
	return n
}

// EffectiveInputTokens returns the total input tokens counted against quota
// across providers: PromptTokens + CacheReadInputTokens + CacheCreationInputTokens.
// For OpenAI/Gemini (where PromptTokens already includes cached reads),
// CacheRead/Create stay zero so the result collapses to PromptTokens.
func (u *Usage) EffectiveInputTokens() int64 {
	if u == nil {
		return 0
	}
	return u.PromptTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// CompletionTokensDetails Breakdown of tokens used in a completion.
type CompletionTokensDetails struct {
	AudioTokens              int64 `json:"audio_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
	AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int64 `json:"rejected_prediction_tokens"`
	// TextTokens / ImageTokens / VideoTokens are populated when upstream providers
	// (Gemini today) report per-modality output breakdowns.
	TextTokens  int64 `json:"text_tokens,omitempty"`
	ImageTokens int64 `json:"image_tokens,omitempty"`
	VideoTokens int64 `json:"video_tokens,omitempty"`
}

// PromptTokensDetails Breakdown of tokens used in the prompt.
type PromptTokensDetails struct {
	AudioTokens  int64 `json:"audio_tokens"`
	CachedTokens int64 `json:"cached_tokens"`
	// TextTokens / ImageTokens / VideoTokens / DocumentTokens are populated
	// when upstream providers (Gemini today) report per-modality input breakdowns.
	TextTokens     int64 `json:"text_tokens,omitempty"`
	ImageTokens    int64 `json:"image_tokens,omitempty"`
	VideoTokens    int64 `json:"video_tokens,omitempty"`
	DocumentTokens int64 `json:"document_tokens,omitempty"`
}

// ModalityTokenCount Represents token counting info for a single modality.
type ModalityTokenCount struct {
	Modality string `json:"modality,omitempty"`
	// Number of tokens.
	TokenCount int64 `json:"token_count,omitempty"`
}
