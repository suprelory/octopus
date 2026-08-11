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
}

// StreamFinalizer owns the provider-independent lifecycle for one upstream
// stream. ProcessStreamEvents is the semantic precommit boundary; FinalizeStream
// closes the lifecycle at EOF and returns the canonical aggregate used by
// metrics and replay.
type StreamFinalizer struct {
	aggregator StreamAggregator

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
}

func NewStreamFinalizer() *StreamFinalizer {
	return &StreamFinalizer{
		started:         make(map[int]bool),
		stopped:         make(map[int]FinishReason),
		toolCallChoices: make(map[int]bool),
	}
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

	normalized := make([]StreamEvent, 0, len(events)+3)
	for _, event := range events {
		if f.done {
			return nil, fmt.Errorf("%w: event %q arrived after done", ErrStreamAlreadyFinalized, event.Kind)
		}
		if event.ID != "" {
			f.lastID = event.ID
		}
		if event.Model != "" {
			f.lastModel = event.Model
		}
		f.sequence++

		switch event.Kind {
		case StreamEventKindError:
			if event.Error == nil {
				return nil, errors.New("stream error event is missing error detail")
			}
			return nil, event.Error

		case StreamEventKindDone:
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
			StreamEventKindContentBlockStart, StreamEventKindToolCallStart, StreamEventKindToolCallDelta:
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
					event.StopReason = FinishReasonStop
				}
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
	if f == nil {
		return nil, errors.New("stream finalizer is nil")
	}
	if f.finalized {
		return nil, ErrStreamAlreadyFinalized
	}

	var tail []StreamEvent
	var err error
	if !f.done {
		tail, err = f.terminalEvents(true)
		if err != nil {
			return nil, err
		}
	}
	f.finalized = true

	reasons := make(map[int]FinishReason, len(f.stopped))
	for index, reason := range f.stopped {
		reasons[index] = reason
	}
	return &StreamFinalization{
		TailEvents:    tail,
		Response:      f.aggregator.Response(),
		Usage:         f.usage,
		FinishReasons: reasons,
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
		reason := FinishReasonStop
		if f.toolCallChoices[index] {
			reason = FinishReasonToolCalls
		}
		stop := StreamEvent{Kind: StreamEventKindMessageStop, ID: f.lastID, Model: f.lastModel, Index: index, StopReason: reason}
		*events = append(*events, stop)
		f.sequence++
		f.lastStopSeq = f.sequence
		f.stopped[index] = reason
		f.addToAggregate(stop)
	}
}

func (f *StreamFinalizer) addToAggregate(event StreamEvent) {
	response := InternalResponseFromStreamEvents([]StreamEvent{event})
	if response != nil && response.Object != "[DONE]" {
		f.aggregator.Add(response)
	}
}
