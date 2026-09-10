package stream

import (
	"bytes"
	"fmt"
)

const defaultMaxSSEEventSize = 32 * 1024 * 1024

// SSEDecoder is the bounded framing state shared by stream sources and raw
// passthrough observers. Feed/Finish have one owner. Emitted data belongs to the
// receiver; pending data is borrowed until the next call. No JSON is parsed here.
type SSEDecoder struct {
	maxEventSize int
	line         []byte
	data         []byte
	eventType    string
	eventID      string
	sequence     int64
	frameBytes   int
	hasData      bool
	skipLF       bool
	firstLine    bool
	finished     bool
	err          error
}

func NewSSEDecoder(maxEventSize int) *SSEDecoder {
	if maxEventSize <= 0 {
		maxEventSize = defaultMaxSSEEventSize
	}
	return &SSEDecoder{maxEventSize: maxEventSize, firstLine: true}
}

func (d *SSEDecoder) account(size int) error {
	if size > d.maxEventSize-d.frameBytes {
		d.err = fmt.Errorf("SSE event exceeds maximum size of %d bytes", d.maxEventSize)
		return d.err
	}
	d.frameBytes += size
	return nil
}

func (d *SSEDecoder) appendBuffer(dst, src []byte) []byte {
	needed := len(dst) + len(src)
	if needed > cap(dst) {
		size := min(d.maxEventSize, max(needed, max(64, cap(dst)*2)))
		next := make([]byte, len(dst), size)
		copy(next, dst)
		dst = next
	}
	return append(dst, src...)
}

func (d *SSEDecoder) Feed(chunk []byte, emit func(SourceEvent) error) error {
	if d.err != nil {
		return d.err
	}
	if d.finished {
		return fmt.Errorf("SSE decoder already finalized")
	}
	for len(chunk) > 0 {
		if d.skipLF {
			d.skipLF = false
			if chunk[0] == '\n' {
				chunk = chunk[1:]
				continue
			}
		}
		end := sseLineEnd(chunk)
		if end < 0 {
			if err := d.account(len(chunk)); err != nil {
				return err
			}
			d.line = d.appendBuffer(d.line, chunk)
			return nil
		}
		if err := d.account(end + 1); err != nil {
			return err
		}
		owned := len(d.line) > 0
		line := chunk[:end]
		if owned {
			line = d.appendBuffer(d.line, line)
			d.line = nil
		}
		d.skipLF = chunk[end] == '\r'
		chunk = chunk[end+1:]
		claimed, err := d.processLine(line, owned, emit)
		if owned && !claimed && cap(line) <= 16*1024 {
			d.line = line[:0]
		}
		if err != nil {
			d.err = err
			return err
		}
	}
	return nil
}

func sseLineEnd(chunk []byte) int {
	if len(chunk) > 64 {
		return bytes.IndexAny(chunk, "\r\n")
	}
	for index, value := range chunk {
		if value == '\r' || value == '\n' {
			return index
		}
	}
	return -1
}

func (d *SSEDecoder) processLine(line []byte, owned bool, emit func(SourceEvent) error) (bool, error) {
	if d.firstLine {
		d.firstLine = false
		line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
	}
	if len(line) == 0 {
		return false, d.dispatch(emit)
	}
	if line[0] == ':' {
		return false, nil
	}
	field, value, found := bytes.Cut(line, []byte{':'})
	if !found {
		field, value = line, nil
	}
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	switch string(field) {
	case "event":
		d.eventType = string(value)
	case "id":
		if bytes.IndexByte(value, 0) < 0 && d.eventID != string(value) {
			d.eventID = string(value)
		}
	case "data":
		if !d.hasData && owned {
			d.hasData, d.data = true, value
			return true, nil
		}
		if d.hasData {
			d.data = d.appendBuffer(d.data, []byte{'\n'})
		}
		d.hasData = true
		d.data = d.appendBuffer(d.data, value)
	}
	return false, nil
}

func (d *SSEDecoder) dispatch(emit func(SourceEvent) error) error {
	if !d.hasData && d.eventType == "" {
		d.frameBytes = 0
		return nil
	}
	d.sequence++
	if len(d.data) == 0 {
		d.data = nil
	}
	event := SourceEvent{Type: d.eventType, Data: d.data, ID: d.eventID, Sequence: d.sequence, Transport: SourceTransportSSE}
	d.eventType, d.data, d.hasData, d.frameBytes = "", nil, false, 0
	if emit != nil {
		return emit(event)
	}
	return nil
}

// Pending returns a prospective event after complete field lines. Its metadata
// may still change before the delimiter, so only stateless previews may use it.
func (d *SSEDecoder) Pending() (SourceEvent, bool) {
	if d.finished || d.err != nil || len(d.line) != 0 || !d.hasData {
		return SourceEvent{}, false
	}
	data := d.data
	if len(data) == 0 {
		data = nil
	}
	return SourceEvent{Type: d.eventType, Data: data, ID: d.eventID, Sequence: d.sequence + 1, Transport: SourceTransportSSE}, true
}

// Finish is only for clean EOF (or a fully delivered terminal on client close).
// An underlying read error must not turn a partial frame into a terminal event.
func (d *SSEDecoder) Finish(emit func(SourceEvent) error) error {
	if d.err != nil {
		return d.err
	}
	if d.finished {
		return nil
	}
	d.finished = true
	if len(d.line) > 0 {
		line := d.line
		d.line = nil
		if _, err := d.processLine(line, true, emit); err != nil {
			d.err = err
			return err
		}
	}
	d.err = d.dispatch(emit)
	return d.err
}
