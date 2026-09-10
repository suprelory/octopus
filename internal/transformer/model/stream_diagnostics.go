package model

// StreamDiagnostics contains bounded evidence for one stream. BytesReceived
// counts provider payload bytes, excluding SSE framing. It never stores content.
type StreamDiagnostics struct {
	LastEventType          string            `json:"last_event_type"`
	LastCanonicalEventType StreamEventKind   `json:"last_canonical_event_type,omitempty"`
	TerminalEventSeen      bool              `json:"terminal_event_seen"`
	FinishReasonSeen       bool              `json:"finish_reason_seen"`
	EventsReceived         int64             `json:"events_received"`
	BytesReceived          int64             `json:"bytes_received"`
	CompletionStatus       string            `json:"completion_status"`
	FinishCause            StreamFinishCause `json:"finish_cause,omitempty"`
	CleanEOF               bool              `json:"clean_eof"`
	SourceTransport        string            `json:"source_transport"`
	LastSourceSequence     int64             `json:"last_source_sequence"`
}

func (c *CanonicalStreamConverter) Diagnostics() StreamDiagnostics {
	diagnostics := c.diagnostics
	if c.finished {
		diagnostics.FinishCause = c.finalizer.FinishCause()
		diagnostics.CompletionStatus = "completed"
		if c.err != nil {
			diagnostics.CompletionStatus = "interrupted"
			if diagnostics.FinishCause == StreamFinishCauseClientCancellation {
				diagnostics.CompletionStatus = "canceled"
			}
		}
	}
	return diagnostics
}
