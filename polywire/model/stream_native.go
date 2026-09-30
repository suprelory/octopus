package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type StreamImportance string

const (
	StreamImportanceLifecycle StreamImportance = "lifecycle"
	StreamImportanceMetadata  StreamImportance = "metadata"
	StreamImportanceSemantic  StreamImportance = "semantic"
	StreamImportanceUsage     StreamImportance = "usage"
)

// StreamProvenance is immutable after publication. A provider frame can produce
// several canonical events, all sharing this envelope without copying its body.
// RawPayload is bytes rather than RawMessage because terminal data need not be JSON.
type StreamProvenance struct {
	Format            APIFormat `json:"format"`
	EventType         string    `json:"event_type,omitempty"`
	ProviderEventType string    `json:"provider_event_type,omitempty"`
	EventID           string    `json:"event_id,omitempty"`
	Sequence          int64     `json:"sequence,omitempty"`
	Transport         string    `json:"transport,omitempty"`
	ProviderSequence  *int64    `json:"provider_sequence,omitempty"`
	RawPayload        []byte    `json:"raw_payload,omitempty"`
}

// StreamNativeEvent keeps response/item identities and native tool phases out
// of chat function calls. Payload preserves the native item or content block,
// while Provenance preserves the complete frame, including unknown fields.
type StreamNativeEvent struct {
	Type         string          `json:"type,omitempty"`
	Phase        string          `json:"phase,omitempty"`
	ID           string          `json:"id,omitempty"`
	OutputIndex  *int            `json:"output_index,omitempty"`
	ContentIndex *int            `json:"content_index,omitempty"`
	CallID       string          `json:"call_id,omitempty"`
	Name         string          `json:"name,omitempty"`
	ServerLabel  string          `json:"server_label,omitempty"`
	Status       string          `json:"status,omitempty"`
	Arguments    string          `json:"arguments,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	// Required means the event has no equivalent in the ordinary chat view.
	Required bool `json:"required,omitempty"`
}

func (e StreamEvent) IsNativeStreamEvent() bool {
	switch e.Kind {
	case StreamEventKindResponseStart, StreamEventKindResponseStop,
		StreamEventKindOutputItemStart, StreamEventKindOutputItemStop,
		StreamEventKindMCPCall, StreamEventKindComputerUse, StreamEventKindServerTool,
		StreamEventKindGrounding:
		return true
	}
	return false
}

func streamImportance(e StreamEvent) StreamImportance {
	if HasSemanticStreamEvents([]StreamEvent{e}) {
		return StreamImportanceSemantic
	}
	switch e.Kind {
	case StreamEventKindMessageMetadata:
		return StreamImportanceMetadata
	case StreamEventKindUsageDelta:
		return StreamImportanceUsage
	case StreamEventKindMessageStart, StreamEventKindMessageStop, StreamEventKindDone,
		StreamEventKindContentBlockStart, StreamEventKindContentBlockStop,
		StreamEventKindToolCallStop, StreamEventKindResponseStart, StreamEventKindResponseStop,
		StreamEventKindOutputItemStart, StreamEventKindOutputItemStop:
		return StreamImportanceLifecycle
	case StreamEventKindMCPCall, StreamEventKindComputerUse, StreamEventKindServerTool, StreamEventKindGrounding:
		return StreamImportanceSemantic
	default:
		return StreamImportanceMetadata
	}
}

func (f *StreamFinalizer) synthesized(event StreamEvent) StreamEvent {
	event.Provenance = f.lastProvenance
	event.Synthesized = true
	event.Importance = streamImportance(event)
	return event
}

func WithStreamSource(events []StreamEvent, source SourceEvent, format APIFormat, providerSequence *int64, providerTypes ...string) []StreamEvent {
	if len(events) == 0 {
		return events
	}
	provenance := &StreamProvenance{Format: format, EventType: source.Type, EventID: source.ID,
		Sequence: source.Sequence, Transport: source.Transport, ProviderSequence: providerSequence,
		RawPayload: bytes.Clone(source.Data)}
	if len(providerTypes) > 0 {
		provenance.ProviderEventType = providerTypes[0]
	}
	for index := range events {
		if events[index].Provenance == nil {
			events[index].Provenance = provenance
		}
		if events[index].Importance == "" {
			events[index].Importance = streamImportance(events[index])
		}
	}
	return events
}

func OpaqueSourceEvent(source SourceEvent) StreamEvent {
	event := StreamEvent{Kind: StreamEventKindOpaque, Importance: StreamImportanceSemantic}
	if json.Valid(source.Data) {
		event.Opaque = bytes.Clone(source.Data)
	}
	return event
}

// StreamReplay reconstructs the original provider envelopes from an ordered
// canonical stream. It never flattens native tools or reserializes their JSON.
// Callers choose replay explicitly; ordinary encoders report unsupported native
// semantics instead of guessing a cross-provider representation.
type StreamReplay struct {
	last *StreamProvenance
}

func (r *StreamReplay) Push(events []StreamEvent, target APIFormat) ([]SourceEvent, error) {
	var replay []SourceEvent
	last := r.last
	for _, event := range events {
		if event.Synthesized {
			continue
		}
		source := event.Provenance
		if source == nil || source.Format != target {
			return nil, NewStreamConversionLoss(event, target, "native replay requires intact provenance in the originating protocol")
		}
		if last == source {
			continue
		}
		if last != nil && source.Sequence > 0 && source.Sequence == last.Sequence && source.Format == last.Format {
			if source.EventType != last.EventType || source.EventID != last.EventID || !bytes.Equal(source.RawPayload, last.RawPayload) {
				return nil, NewStreamConversionLoss(event, target, "source sequence identifies conflicting provider frames")
			}
			continue
		}
		if last != nil && source.Sequence > 0 && last.Sequence > source.Sequence {
			return nil, NewStreamConversionLoss(event, target, "source sequence regressed during native replay")
		}
		replay = append(replay, SourceEvent{Type: source.EventType, ID: source.EventID,
			Data: bytes.Clone(source.RawPayload), Sequence: source.Sequence, Transport: source.Transport})
		last = source
	}
	r.last = last
	return replay, nil
}

// StreamConversionLoss is a structured, actionable failure for a native event
// that a selected wire encoder cannot represent. Relay logs retain the details.
type StreamConversionLoss struct {
	Kind           StreamEventKind `json:"kind"`
	SourceFormat   APIFormat       `json:"source_format,omitempty"`
	TargetFormat   APIFormat       `json:"target_format"`
	SourceSequence int64           `json:"source_sequence,omitempty"`
	EventType      string          `json:"event_type,omitempty"`
	Reason         string          `json:"reason"`
}

func NewStreamConversionLoss(event StreamEvent, target APIFormat, reason string) *StreamConversionLoss {
	loss := &StreamConversionLoss{Kind: event.Kind, TargetFormat: target, Reason: reason}
	if event.Provenance != nil {
		loss.SourceFormat, loss.SourceSequence, loss.EventType = event.Provenance.Format, event.Provenance.Sequence, event.Provenance.EventType
		if loss.EventType == "" {
			loss.EventType = event.Provenance.ProviderEventType
		}
	}
	return loss
}

func (e *StreamConversionLoss) Error() string {
	return fmt.Sprintf("stream conversion loss: %s (%s, sequence %d) to %s: %s", e.Kind, e.EventType, e.SourceSequence, e.TargetFormat, e.Reason)
}

func ValidateNativeStreamEncoding(events []StreamEvent, target APIFormat) error {
	for _, event := range events {
		if event.Kind == StreamEventKindMCPCall || event.Kind == StreamEventKindServerTool {
			if target != APIFormatAnthropicMessage || event.Provenance == nil || event.Provenance.Format != APIFormatAnthropicMessage {
				return NewStreamConversionLoss(event, target, "native tool execution cannot be represented as a client function call")
			}
		}
		if event.Kind == StreamEventKindCitationDelta && event.Delta != nil && event.Delta.Citation != nil {
			citation := *event.Delta.Citation
			if target == APIFormatAnthropicMessage {
				if citation.Provider != "" && citation.Provider != string(SignatureProviderAnthropic) || citation.Type == "" {
					return NewStreamConversionLoss(event, target, "citation lacks an Anthropic source location")
				}
			} else if _, err := OpenAICitationForWire(citation, target); err != nil {
				return NewStreamConversionLoss(event, target, err.Error())
			}
		}
		if event.Kind == StreamEventKindOpaque || (event.Native != nil && event.Native.Required) {
			return NewStreamConversionLoss(event, target, "event requires native source replay or passthrough")
		}
		if event.Kind == StreamEventKindGrounding {
			return NewStreamConversionLoss(event, target, "target has no lossless grounding event")
		}
		if event.Kind == StreamEventKindAudioDelta && event.Media != nil && event.Media.Placement == "" {
			return NewStreamConversionLoss(event, target, "native audio requires its original media protocol")
		}
	}
	return nil
}
