package model

import (
	"strings"
)

// ReasoningBlockKind enumerates the kinds of reasoning/thinking blocks we preserve.
type ReasoningBlockKind string

const (
	// ReasoningBlockKindThinking is a visible thinking block with optional signature (Anthropic)
	// or thought-signature-carrying Part (Gemini 3).
	ReasoningBlockKindThinking ReasoningBlockKind = "thinking"
	// ReasoningBlockKindRedacted is Anthropic's redacted_thinking block (opaque data, no text).
	ReasoningBlockKindRedacted ReasoningBlockKind = "redacted_thinking"
	// ReasoningBlockKindSignature is a standalone signature carrier used when the text part has
	// already been emitted separately (e.g. Gemini fn-call Part-level thoughtSignature).
	ReasoningBlockKindSignature ReasoningBlockKind = "thought_signature"
)

type SignatureProvider string

const (
	SignatureProviderAnthropic SignatureProvider = "anthropic"
	SignatureProviderGemini    SignatureProvider = "gemini"
	SignatureProviderOpenAI    SignatureProvider = "openai"
)

type OpaqueSignatureKind string

const (
	OpaqueSignatureKindAnthropicThinking OpaqueSignatureKind = "anthropic_thinking"
	OpaqueSignatureKindGeminiThought     OpaqueSignatureKind = "gemini_thought"
	OpaqueSignatureKindOpenAIReasoning   OpaqueSignatureKind = "openai_reasoning"
)

// SignatureToolCallScope binds a provider signature to the tool call that
// produced it. Empty fields mean the signature belongs to the reasoning block
// itself rather than a tool call.
type SignatureToolCallScope struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// OpaqueSignature carries provider-owned signature bytes with their source
// and semantic kind. Value must never be reinterpreted for another provider.
type OpaqueSignature struct {
	Provider      SignatureProvider       `json:"provider"`
	Kind          OpaqueSignatureKind     `json:"kind"`
	Value         string                  `json:"value"`
	ToolCallScope *SignatureToolCallScope `json:"tool_call_scope,omitempty"`
}

func (s OpaqueSignature) ValidFor(provider SignatureProvider) bool {
	return strings.TrimSpace(s.Value) != "" && s.Provider == provider
}

func (s OpaqueSignature) ValidForKind(provider SignatureProvider, kind OpaqueSignatureKind) bool {
	return s.ValidFor(provider) && s.Kind == kind
}

func SignatureKindForProvider(provider SignatureProvider) OpaqueSignatureKind {
	switch provider {
	case SignatureProviderAnthropic:
		return OpaqueSignatureKindAnthropicThinking
	case SignatureProviderGemini:
		return OpaqueSignatureKindGeminiThought
	case SignatureProviderOpenAI:
		return OpaqueSignatureKindOpenAIReasoning
	default:
		return ""
	}
}

func CloneOpaqueSignature(signature *OpaqueSignature) *OpaqueSignature {
	if signature == nil {
		return nil
	}
	copy := *signature
	if signature.ToolCallScope != nil {
		scope := *signature.ToolCallScope
		copy.ToolCallScope = &scope
	}
	return &copy
}

// ReasoningBlock preserves one thinking/redacted_thinking/thought_signature block verbatim.
// All fields are opaque to the aggregator; they are round-tripped to the upstream as-is.
type ReasoningBlock struct {
	Kind  ReasoningBlockKind `json:"kind,omitempty"`
	Index int                `json:"index,omitempty"`
	Text  string             `json:"text,omitempty"`
	// Signature is a compatibility mirror. SignatureSource is authoritative
	// when present and prevents provider-owned opaque bytes crossing providers.
	Signature       string           `json:"signature,omitempty"`
	SignatureSource *OpaqueSignature `json:"-"`
	Data            string           `json:"data,omitempty"`
	Provider        string           `json:"provider,omitempty"`

	// ToolCallID / ToolCallName anchor a Signature-kind block to the
	// specific function call it belongs to. Gemini 3 returns one
	// thoughtSignature per functionCall, and the outbound layer must replay
	// the signature on the matching functionCall part by name (not by
	// ordinal position) — otherwise multi-tool turns get their signatures
	// swapped and Gemini rejects the replay with 400. See G-H7.
	ToolCallID   string `json:"tool_call_id,omitempty"`
	ToolCallName string `json:"tool_call_name,omitempty"`
}

func (b ReasoningBlock) OpaqueSignature() (OpaqueSignature, bool) {
	if b.SignatureSource != nil {
		if strings.TrimSpace(b.SignatureSource.Value) == "" {
			return OpaqueSignature{}, false
		}
		signature := *b.SignatureSource
		if signature.ToolCallScope != nil {
			scope := *signature.ToolCallScope
			signature.ToolCallScope = &scope
		}
		return signature, true
	}
	if strings.TrimSpace(b.Signature) == "" {
		return OpaqueSignature{}, false
	}
	signature := OpaqueSignature{
		Provider: SignatureProvider(strings.TrimSpace(b.Provider)),
		Value:    b.Signature,
	}
	switch signature.Provider {
	case SignatureProviderAnthropic:
		signature.Kind = OpaqueSignatureKindAnthropicThinking
	case SignatureProviderGemini:
		signature.Kind = OpaqueSignatureKindGeminiThought
	case SignatureProviderOpenAI:
		signature.Kind = OpaqueSignatureKindOpenAIReasoning
	}
	if b.ToolCallID != "" || b.ToolCallName != "" {
		signature.ToolCallScope = &SignatureToolCallScope{ID: b.ToolCallID, Name: b.ToolCallName}
	}
	return signature, true
}

func (b *ReasoningBlock) SetOpaqueSignature(signature OpaqueSignature) {
	if b == nil {
		return
	}
	copy := signature
	if signature.ToolCallScope != nil {
		scope := *signature.ToolCallScope
		copy.ToolCallScope = &scope
		b.ToolCallID = scope.ID
		b.ToolCallName = scope.Name
	}
	b.SignatureSource = &copy
	b.Signature = signature.Value
	b.Provider = string(signature.Provider)
}

// AppendReasoningBlock appends a reasoning block preserving insertion order.
// Index is auto-assigned based on the current slice length when the caller passes a negative Index.
func (m *Message) AppendReasoningBlock(block ReasoningBlock) {
	if block.SignatureSource == nil && strings.TrimSpace(block.Signature) != "" && strings.TrimSpace(block.Provider) != "" {
		signature, _ := block.OpaqueSignature()
		block.SetOpaqueSignature(signature)
	}
	if block.Index < 0 {
		block.Index = len(m.ReasoningBlocks)
	}
	m.ReasoningBlocks = append(m.ReasoningBlocks, block)
}

// ReasoningBlocksByProvider returns the subset of blocks authored by the given provider.
// Pass an empty string to get the full slice.
func (m *Message) ReasoningBlocksByProvider(provider string) []ReasoningBlock {
	if provider == "" {
		return m.ReasoningBlocks
	}
	out := make([]ReasoningBlock, 0, len(m.ReasoningBlocks))
	for _, b := range m.ReasoningBlocks {
		blockProvider := b.Provider
		if b.SignatureSource != nil {
			blockProvider = string(b.SignatureSource.Provider)
		}
		if blockProvider == provider {
			out = append(out, b)
		}
	}
	return out
}

// OpaqueSignaturesByProvider returns only signatures explicitly attributed to
// provider. Untagged legacy flat signatures are intentionally excluded.
func (m *Message) OpaqueSignaturesByProvider(provider SignatureProvider) []OpaqueSignature {
	if m == nil || provider == "" {
		return nil
	}
	kind := SignatureKindForProvider(provider)
	if kind == "" {
		return nil
	}
	out := make([]OpaqueSignature, 0, len(m.ReasoningBlocks)+1)
	seen := make(map[string]struct{}, len(m.ReasoningBlocks)+1)
	appendSignature := func(signature OpaqueSignature) {
		if !signature.ValidForKind(provider, kind) {
			return
		}
		key := string(signature.Kind) + "\x00" + signature.Value
		if signature.ToolCallScope != nil {
			key += "\x00" + signature.ToolCallScope.ID + "\x00" + signature.ToolCallScope.Name
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, signature)
	}
	for _, block := range m.ReasoningBlocks {
		if signature, ok := block.OpaqueSignature(); ok {
			appendSignature(signature)
		}
	}
	if m.ReasoningSignatureSource != nil {
		appendSignature(*m.ReasoningSignatureSource)
	}
	return out
}

// SetOpaqueReasoningSignature updates both the authoritative provenance and
// the deprecated flat string mirror used by legacy callers.
func (m *Message) SetOpaqueReasoningSignature(signature OpaqueSignature) {
	if m == nil {
		return
	}
	copy := signature
	if signature.ToolCallScope != nil {
		scope := *signature.ToolCallScope
		copy.ToolCallScope = &scope
	}
	m.ReasoningSignatureSource = &copy
	value := signature.Value
	m.ReasoningSignature = &value
}
