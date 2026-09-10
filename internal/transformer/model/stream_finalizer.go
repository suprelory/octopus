package model

import (
	"errors"
	"fmt"
	"sort"
)

var (
	ErrStreamAlreadyFinalized = errors.New("stream already finalized")
	ErrStreamIncomplete       = errors.New("stream ended without a finish reason")
)

type StreamFinalization struct {
	TailEvents    []StreamEvent
	Response      *InternalLLMResponse
	Usage         *Usage
	FinishReasons map[int]FinishReason
	FinishCause   StreamFinishCause
	TerminalEvent string
}

// StreamFinalizer owns the provider-independent lifecycle for one upstream
// stream. ProcessStreamEvents is the semantic precommit boundary; FinalizeStream
// closes the lifecycle at EOF and returns the canonical aggregate used by
// metrics and replay.
type StreamFinalizer struct {
	aggregator StreamAggregator
	policy     StreamTerminalPolicy

	started         map[int]bool
	stopped         map[int]FinishReason
	lastID          string
	lastModel       string
	usage           *Usage
	sequence        int
	lastStopSeq     int
	lastUsageSeq    int
	toolCallChoices map[int]bool
	done            bool
	finalized       bool
	terminalSeen    bool
	terminalEvent   string
	seenKinds       map[StreamEventKind]bool
	finishCause     StreamFinishCause
	failure         error
}

func NewStreamFinalizer(policies ...StreamTerminalPolicy) *StreamFinalizer {
	policy := DefaultStreamTerminalPolicy()
	if len(policies) > 0 {
		policy = policies[0].normalized()
	}
	return &StreamFinalizer{
		policy:          policy,
		started:         make(map[int]bool),
		stopped:         make(map[int]FinishReason),
		toolCallChoices: make(map[int]bool),
		seenKinds:       make(map[StreamEventKind]bool),
	}
}

// SetTerminalPolicy updates the policy before the first event is processed.
// It keeps lazy relay construction compatible with the historical constructor.
func (f *StreamFinalizer) SetTerminalPolicy(policy StreamTerminalPolicy) {
	if f == nil || f.sequence != 0 || f.finalized {
		return
	}
	f.policy = policy.normalized()
}

func (f *StreamFinalizer) TerminalSeen() bool {
	return f != nil && f.terminalSeen
}

func (f *StreamFinalizer) FinishCause() StreamFinishCause {
	if f == nil {
		return StreamFinishCauseSourceError
	}
	if f.finishCause != "" {
		return f.finishCause
	}
	if f.terminalSeen {
		return StreamFinishCauseExplicitTerminal
	}
	return StreamFinishCauseCleanEOF
}

// ProcessStreamEvents validates and normalizes events before any corresponding
// downstream bytes are written. Provider error events become ordinary Go
// errors so relay can still fail over while the stream is uncommitted.
func (f *StreamFinalizer) ProcessStreamEvents(events []StreamEvent) ([]StreamEvent, error) {
	if f == nil {
		return nil, errors.New("stream finalizer is nil")
	}
	if f.finalized {
		return nil, ErrStreamAlreadyFinalized
	}
	if f.failure != nil {
		return nil, f.failure
	}

	normalized := make([]StreamEvent, 0, len(events)+3)
	for _, event := range events {
		f.seenKinds[event.Kind] = true
		if event.Kind == StreamEventKindError {
			f.failure = event.Error
			if event.Error == nil {
				f.failure = errors.New("stream error event is missing error detail")
			}
			return nil, f.failure
		}
		if f.done {
			// Duplicate provider terminal markers are harmless. Ignore them so
			// an explicit terminal cannot duplicate downstream completion frames.
			if event.Terminal || event.Kind == StreamEventKindDone || event.Kind == StreamEventKindMessageStop {
				continue
			}
			return nil, fmt.Errorf("%w: event %q arrived after done", ErrStreamAlreadyFinalized, event.Kind)
		}
		if event.Terminal {
			f.terminalSeen = true
			if event.TerminalEvent != "" {
				f.terminalEvent = event.TerminalEvent
			}
		}
		if event.ID != "" {
			f.lastID = event.ID
		}
		if event.Model != "" {
			f.lastModel = event.Model
		}
		f.sequence++

		switch event.Kind {
		case StreamEventKindDone:
			f.terminalSeen = true
			f.synthesizeMissingStops(&normalized)
			tail, err := f.terminalEvents(true)
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, tail...)
			continue

		case StreamEventKindMessageStart:
			f.started[event.Index] = true

		case StreamEventKindTextDelta, StreamEventKindThinkingDelta, StreamEventKindSignatureDelta,
			StreamEventKindContentBlockStart, StreamEventKindContentBlockDelta, StreamEventKindToolCallStart, StreamEventKindToolCallDelta,
			StreamEventKindImageDelta, StreamEventKindAudioDelta, StreamEventKindOpaque,
			StreamEventKindCitationDelta:
			if !f.started[event.Index] {
				start := StreamEvent{Kind: StreamEventKindMessageStart, ID: event.ID, Model: event.Model, Index: event.Index, Role: "assistant"}
				normalized = append(normalized, start)
				f.addToAggregate(start)
				f.started[event.Index] = true
			}
			if event.Kind == StreamEventKindToolCallStart || event.Kind == StreamEventKindToolCallDelta {
				f.toolCallChoices[event.Index] = true
			}

		case StreamEventKindUsageDelta:
			if event.Usage == nil {
				return nil, errors.New("usage event is missing usage detail")
			}
			f.usage = event.Usage
			f.lastUsageSeq = f.sequence

		case StreamEventKindMessageStop:
			if !f.started[event.Index] {
				start := StreamEvent{Kind: StreamEventKindMessageStart, ID: event.ID, Model: event.Model, Index: event.Index, Role: "assistant"}
				normalized = append(normalized, start)
				f.addToAggregate(start)
				f.started[event.Index] = true
			}
			if event.StopReason.IsZero() {
				if f.toolCallChoices[event.Index] {
					event.StopReason = FinishReasonToolCalls
				} else {
					event.StopReason = f.policy.DefaultFinishReason
				}
			}
			if previous, exists := f.stopped[event.Index]; exists {
				// A duplicate stop is idempotent. Preserve the first explicit
				// reason unless the duplicate supplies the only non-zero reason.
				if previous.IsZero() && !event.StopReason.IsZero() {
					f.stopped[event.Index] = event.StopReason
				}
				continue
			}
			f.stopped[event.Index] = event.StopReason
			f.lastStopSeq = f.sequence

		case StreamEventKindContentBlockStop, StreamEventKindToolCallStop:
			// Structural boundaries do not change message completion state.

		default:
			return nil, fmt.Errorf("unknown stream event kind %q", event.Kind)
		}

		normalized = append(normalized, event)
		f.addToAggregate(event)
	}
	return normalized, nil
}

// FinalizeStream completes a stream that ended at EOF without a provider done
// marker. TailEvents must be encoded through the inbound adapter before the
// downstream stream is closed.
func (f *StreamFinalizer) FinalizeStream() (*StreamFinalization, error) {
	return f.Finish(StreamFinishCauseCleanEOF)
}

// Finish completes the canonical lifecycle for the supplied termination cause.
// Source errors and client cancellation never synthesize successful completion.
func (f *StreamFinalizer) Finish(cause StreamFinishCause) (*StreamFinalization, error) {
	if f == nil {
		return nil, errors.New("stream finalizer is nil")
	}
	if f.finalized {
		return nil, ErrStreamAlreadyFinalized
	}
	defer func() { f.finalized = true }()

	if cause == "" {
		cause = StreamFinishCauseCleanEOF
	}
	if cause == StreamFinishCauseExplicitTerminal {
		f.terminalSeen = true
	}
	if cause == StreamFinishCauseSourceError || cause == StreamFinishCauseClientCancellation {
		f.finishCause = cause
		return nil, fmt.Errorf("%w: stream finish cause %s", ErrStreamIncomplete, cause)
	}
	if f.failure != nil {
		f.finishCause = StreamFinishCauseSourceError
		return nil, f.failure
	}
	if f.terminalSeen {
		cause = StreamFinishCauseExplicitTerminal
	}
	f.finishCause = cause

	var tail []StreamEvent
	if !f.done {
		if cause == StreamFinishCauseCleanEOF && !f.policy.CleanEOFCompletes {
			if err := f.validateCompletion(); err != nil {
				return nil, fmt.Errorf("%w: clean EOF without terminal event", err)
			}
		}
		if cause == StreamFinishCauseExplicitTerminal || f.policy.CleanEOFCompletes {
			f.synthesizeMissingStops(&tail)
		}
		terminalTail, err := f.terminalEvents(true)
		if err != nil {
			return nil, err
		}
		tail = append(tail, terminalTail...)
	}

	reasons := make(map[int]FinishReason, len(f.stopped))
	for index, reason := range f.stopped {
		reasons[index] = reason
	}
	return &StreamFinalization{
		TailEvents:    tail,
		Response:      f.aggregator.Response(),
		Usage:         f.usage,
		FinishReasons: reasons,
		FinishCause:   cause,
		TerminalEvent: f.terminalEvent,
	}, nil
}

func (f *StreamFinalizer) terminalEvents(includeDone bool) ([]StreamEvent, error) {
	if err := f.validateCompletion(); err != nil {
		return nil, err
	}
	tail := make([]StreamEvent, 0, 2)
	if len(f.stopped) > 0 && f.lastUsageSeq < f.lastStopSeq {
		usage := f.usage
		if usage == nil {
			usage = &Usage{}
		}
		usageEvent := StreamEvent{Kind: StreamEventKindUsageDelta, ID: f.lastID, Model: f.lastModel, Usage: usage}
		tail = append(tail, usageEvent)
		f.sequence++
		f.lastUsageSeq = f.sequence
		f.usage = usage
		f.addToAggregate(usageEvent)
	}
	if includeDone {
		done := StreamEvent{Kind: StreamEventKindDone, ID: f.lastID, Model: f.lastModel}
		tail = append(tail, done)
		f.done = true
	}
	return tail, nil
}

func (f *StreamFinalizer) validateCompletion() error {
	for _, required := range f.policy.RequiredLifecycleEvents {
		if !f.seenKinds[required] {
			return fmt.Errorf("%w: missing required lifecycle event %q", ErrStreamIncomplete, required)
		}
	}
	if len(f.started) == 0 {
		return ErrStreamIncomplete
	}
	missing := make([]int, 0)
	for index := range f.started {
		if _, ok := f.stopped[index]; !ok {
			missing = append(missing, index)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Ints(missing)
	return fmt.Errorf("%w for choices %v", ErrStreamIncomplete, missing)
}

func (f *StreamFinalizer) synthesizeMissingStops(events *[]StreamEvent) {
	indices := make([]int, 0, len(f.started))
	for index := range f.started {
		if _, ok := f.stopped[index]; !ok {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	for _, index := range indices {
		reason := f.policy.DefaultFinishReason
		if f.toolCallChoices[index] {
			reason = FinishReasonToolCalls
		}
		stop := StreamEvent{Kind: StreamEventKindMessageStop, ID: f.lastID, Model: f.lastModel, Index: index, StopReason: reason}
		*events = append(*events, stop)
		f.seenKinds[StreamEventKindMessageStop] = true
		f.sequence++
		f.lastStopSeq = f.sequence
		f.stopped[index] = reason
		f.addToAggregate(stop)
	}
}

func (f *StreamFinalizer) addToAggregate(event StreamEvent) {
	f.seenKinds[event.Kind] = true
	response := InternalResponseFromStreamEvents([]StreamEvent{event})
	if response != nil && response.Object != "[DONE]" {
		f.aggregator.Add(response)
	}
}
