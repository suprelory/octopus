package model

import (
	"context"
	"errors"
)

// StreamConverter is one upstream stream's canonical conversion lifecycle.
// A caller serializes Push and Finish. Finish seals the converter for every
// cause; repeating Finish cannot emit another downstream tail.
type StreamConverter interface {
	Push(context.Context, SourceEvent) ([]StreamEvent, error)
	Finish(context.Context, StreamFinishCause) ([]StreamEvent, error)
	Completed() bool
}

type SourceEventTransformer interface {
	TransformSourceEvent(context.Context, SourceEvent) ([]StreamEvent, error)
}

// CanonicalStreamConverter keeps provider parsing in its adapter and delegates
// all canonical lifecycle validation and aggregation to one finalizer.
type CanonicalStreamConverter struct {
	parser    SourceEventTransformer
	finalizer *StreamFinalizer
	result    *StreamFinalization
	finished  bool
	err       error
}

func NewStreamConverter(parser SourceEventTransformer, policy StreamTerminalPolicy) *CanonicalStreamConverter {
	return &CanonicalStreamConverter{parser: parser, finalizer: NewStreamFinalizer(policy)}
}

func (c *CanonicalStreamConverter) Push(ctx context.Context, event SourceEvent) ([]StreamEvent, error) {
	if c.finished {
		return nil, ErrStreamAlreadyFinalized
	}
	if c.err != nil {
		return nil, c.err
	}
	if c.parser == nil {
		c.err = errors.New("stream converter has no provider parser")
		return nil, c.err
	}
	events, err := c.parser.TransformSourceEvent(ctx, event)
	if err == nil {
		events, err = c.finalizer.ProcessStreamEvents(events)
	}
	if err != nil {
		c.err = err
		return nil, err
	}
	return events, nil
}

func (c *CanonicalStreamConverter) Finish(_ context.Context, cause StreamFinishCause) ([]StreamEvent, error) {
	if c.finished {
		return nil, c.err
	}
	c.finished = true
	if c.err != nil {
		cause = StreamFinishCauseSourceError
	}
	result, err := c.finalizer.Finish(cause)
	if c.err == nil {
		c.err = err
	}
	if c.err != nil {
		return nil, c.err
	}
	c.result = result
	return result.TailEvents, nil
}

func (c *CanonicalStreamConverter) Completed() bool                   { return c != nil && c.finished }
func (c *CanonicalStreamConverter) Finalization() *StreamFinalization { return c.result }
func (c *CanonicalStreamConverter) TerminalSeen() bool                { return c.finalizer.TerminalSeen() }
func (c *CanonicalStreamConverter) FinishCause() StreamFinishCause    { return c.finalizer.FinishCause() }

var _ StreamConverter = (*CanonicalStreamConverter)(nil)
