package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SSEEventObserver func(ctx context.Context, eventType string, data []byte) error

// IncrementalSSEObserver parses arbitrary byte chunks without retaining the
// complete stream. Only the current SSE event is buffered, bounded by
// maxEventSize.
type IncrementalSSEObserver struct {
	observe      SSEEventObserver
	terminal     map[string]struct{}
	maxEventSize int

	pendingLine []byte
	eventType   string
	data        bytes.Buffer
	terminalHit bool
	finalized   bool
}

func NewIncrementalSSEObserver(maxEventSize int, terminal map[string]struct{}, observe SSEEventObserver) *IncrementalSSEObserver {
	if maxEventSize <= 0 {
		maxEventSize = 32 * 1024 * 1024
	}
	return &IncrementalSSEObserver{
		observe:      observe,
		terminal:     terminal,
		maxEventSize: maxEventSize,
	}
}

func (o *IncrementalSSEObserver) Observe(ctx context.Context, chunk []byte) error {
	if o == nil || len(chunk) == 0 {
		return nil
	}
	if o.finalized {
		return fmt.Errorf("SSE observer already finalized")
	}
	o.pendingLine = append(o.pendingLine, chunk...)
	for {
		newline := bytes.IndexByte(o.pendingLine, '\n')
		if newline < 0 {
			if o.data.Len()+len(o.pendingLine) > o.maxEventSize {
				return fmt.Errorf("SSE event exceeds maximum size of %d bytes", o.maxEventSize)
			}
			return nil
		}
		line := o.pendingLine[:newline]
		o.pendingLine = o.pendingLine[newline+1:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if err := o.processLine(ctx, line); err != nil {
			return err
		}
	}
}

func (o *IncrementalSSEObserver) Finalize(ctx context.Context) error {
	if o == nil || o.finalized {
		return nil
	}
	o.finalized = true
	if len(o.pendingLine) > 0 {
		line := o.pendingLine
		o.pendingLine = nil
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if err := o.processLine(ctx, line); err != nil {
			return err
		}
	}
	return o.dispatch(ctx)
}

func (o *IncrementalSSEObserver) ReachedTerminal() bool {
	return o != nil && o.terminalHit
}

func (o *IncrementalSSEObserver) processLine(ctx context.Context, line []byte) error {
	if len(line) == 0 {
		return o.dispatch(ctx)
	}
	if line[0] == ':' {
		return nil
	}
	field, value, found := bytes.Cut(line, []byte{':'})
	if !found {
		field = line
		value = nil
	}
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	switch string(field) {
	case "event":
		o.eventType = string(value)
		if len(o.eventType)+o.data.Len() > o.maxEventSize {
			return fmt.Errorf("SSE event exceeds maximum size of %d bytes", o.maxEventSize)
		}
	case "data":
		if o.data.Len() > 0 {
			o.data.WriteByte('\n')
		}
		o.data.Write(value)
		if o.data.Len() > o.maxEventSize {
			return fmt.Errorf("SSE event exceeds maximum size of %d bytes", o.maxEventSize)
		}
		o.detectTerminal()
		// Provider SSE payloads are JSON objects in a single data field. Dispatch
		// as soon as that field is complete so a provider that flushes one newline
		// and stalls cannot deadlock semantic precommit. Incomplete/multiline JSON
		// continues buffering until the blank event delimiter.
		trimmed := bytes.TrimSpace(o.data.Bytes())
		if len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed) {
			return o.dispatch(ctx)
		}
	}
	return nil
}

func (o *IncrementalSSEObserver) detectTerminal() {
	typ := strings.TrimSpace(o.eventType)
	if typ == "" {
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(o.data.Bytes(), &envelope) == nil {
			typ = strings.TrimSpace(envelope.Type)
		}
	}
	if _, ok := o.terminal[typ]; ok {
		o.terminalHit = true
	}
}

func (o *IncrementalSSEObserver) dispatch(ctx context.Context) error {
	if o.eventType == "" && o.data.Len() == 0 {
		return nil
	}
	typ := strings.TrimSpace(o.eventType)
	data := append([]byte(nil), o.data.Bytes()...)
	if typ == "" {
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &envelope) == nil {
			typ = strings.TrimSpace(envelope.Type)
		}
	}
	o.detectTerminal()
	o.eventType = ""
	o.data.Reset()
	if o.observe != nil {
		return o.observe(ctx, typ, data)
	}
	return nil
}
