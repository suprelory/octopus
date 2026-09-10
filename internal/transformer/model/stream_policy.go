package model

// StreamFinishCause records why an upstream stream stopped producing events.
// Keeping this separate from the transport error lets callers distinguish a
// provider terminal marker from a normal reader EOF and an interrupted source.
type StreamFinishCause string

const (
	StreamFinishCauseExplicitTerminal   StreamFinishCause = "explicit_terminal"
	StreamFinishCauseCleanEOF           StreamFinishCause = "clean_eof"
	StreamFinishCauseSourceError        StreamFinishCause = "source_error"
	StreamFinishCauseClientCancellation StreamFinishCause = "client_cancellation"
)

// StreamTerminalPolicy describes the terminal lifecycle declared by a
// provider protocol. TerminalEvents contains provider envelope names; the
// outbound adapter marks the corresponding canonical event as Terminal.
type StreamTerminalPolicy struct {
	TerminalEvents          map[string]struct{} `json:"terminal_events,omitempty"`
	RequiredLifecycleEvents []StreamEventKind   `json:"required_lifecycle_events,omitempty"`
	DefaultFinishReason     FinishReason        `json:"default_finish_reason,omitempty"`
	CleanEOFCompletes       bool                `json:"clean_eof_completes,omitempty"`
}

// DefaultStreamTerminalPolicy keeps the zero-configuration finalizer
// compatible with existing callers: a clean EOF is accepted only when all
// started choices already supplied a stop event.
func DefaultStreamTerminalPolicy() StreamTerminalPolicy {
	return StreamTerminalPolicy{
		DefaultFinishReason: FinishReasonStop,
	}
}

func (p StreamTerminalPolicy) normalized() StreamTerminalPolicy {
	if p.DefaultFinishReason.IsZero() {
		p.DefaultFinishReason = FinishReasonStop
	}
	return p
}

func (p StreamTerminalPolicy) IsTerminalEvent(name string) bool {
	if len(p.TerminalEvents) == 0 {
		return false
	}
	_, ok := p.TerminalEvents[name]
	return ok
}

// StreamTerminalPolicyProvider is implemented by adapters or registries that
// expose protocol-specific terminal rules to relay lifecycle code.
type StreamTerminalPolicyProvider interface {
	StreamTerminalPolicy() StreamTerminalPolicy
}
