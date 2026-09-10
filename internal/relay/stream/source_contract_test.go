package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type contractChunkReader struct {
	io.Reader
	size int
}

func (r contractChunkReader) Read(p []byte) (int, error) {
	if len(p) > r.size {
		p = p[:r.size]
	}
	return r.Reader.Read(p)
}

func TestSourceContractSplitCRLFMultilineAndEmptyTerminal(t *testing.T) {
	const wire = ": comment\r\nid: evt-7\r\nevent: delta\r\ndata: {\"x\":\r\ndata: 1}\r\n\r\nevent: done\r\ndata:\r\n\r\n"
	for _, size := range []int{1, 2, 7, 64} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			source := NewSSESource(io.NopCloser(contractChunkReader{strings.NewReader(wire), size}), 1024)
			defer source.Close()
			var transformed []SourceEvent
			for {
				event, err := source.ReadSourceEvent(context.Background())
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				transformed = append(transformed, event)
			}
			var observed []SourceEvent
			observer := NewIncrementalSourceEventObserver(1024, map[string]struct{}{"done": {}}, func(_ context.Context, event SourceEvent) error { observed = append(observed, event); return nil })
			for offset := 0; offset < len(wire); offset += size {
				if err := observer.Observe(context.Background(), []byte(wire[offset:min(offset+size, len(wire))])); err != nil {
					t.Fatal(err)
				}
			}
			if err := observer.Finalize(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, events := range [][]SourceEvent{transformed, observed} {
				if len(events) != 2 || events[0].Type != "delta" || string(events[0].Data) != "{\"x\":\n1}" || events[1].Type != "done" || len(events[1].Data) != 0 {
					t.Fatalf("framing = %+v", events)
				}
				for index, event := range events {
					if event.ID != "evt-7" || event.Sequence != int64(index+1) || event.Transport != SourceTransportSSE {
						t.Fatalf("metadata = %+v", event)
					}
				}
			}
			if !observer.ReachedTerminal() {
				t.Fatal("terminal metadata lost")
			}
		})
	}
}

type contractCountedReader struct {
	io.ReadCloser
	active, overlap, closes atomic.Int32
}

func (r *contractCountedReader) Read(p []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Add(1)
	}
	defer r.active.Add(-1)
	return r.ReadCloser.Read(p)
}
func (r *contractCountedReader) Close() error { r.closes.Add(1); return r.ReadCloser.Close() }

func TestSourceContractConcurrentReadsAndClose(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			reader, writer := io.Pipe()
			counted := &contractCountedReader{ReadCloser: reader}
			var source interface {
				ReadSourceEvent(context.Context) (SourceEvent, error)
				Close() error
			}
			if raw {
				source = NewRawSource(counted, 1)
			} else {
				source = NewSSESource(counted, 1024)
			}
			defer source.Close()
			defer writer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var readers sync.WaitGroup
			events := make(chan SourceEvent, 100)
			for i := 0; i < 4; i++ {
				readers.Add(1)
				go func() {
					defer readers.Done()
					for {
						event, err := source.ReadSourceEvent(ctx)
						if err != nil {
							return
						}
						events <- event
					}
				}()
			}
			for i := 0; i < 20; i++ {
				frame := "x"
				if !raw {
					frame = fmt.Sprintf("id: %d\nevent: delta\ndata: x\n\n", i)
				}
				if _, err := io.WriteString(writer, frame); err != nil {
					t.Fatal(err)
				}
			}
			var closers sync.WaitGroup
			for i := 0; i < 4; i++ {
				closers.Add(1)
				go func() { defer closers.Done(); _ = source.Close() }()
			}
			closers.Wait()
			done := make(chan struct{})
			go func() { readers.Wait(); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("close did not unblock concurrent readers")
			}
			close(events)
			seen := make(map[int64]bool)
			for event := range events {
				if seen[event.Sequence] || event.Sequence < 1 {
					t.Fatalf("duplicate source sequence: %+v", event)
				}
				seen[event.Sequence] = true
			}
			if counted.overlap.Load() != 0 || counted.closes.Load() != 1 {
				t.Fatalf("overlapping reads=%d close calls=%d", counted.overlap.Load(), counted.closes.Load())
			}
		})
	}
}

func TestSourceContractWebSocketSerializesReaders(t *testing.T) {
	reader := &testWSReader{}
	for i := 0; i < 50; i++ {
		reader.events = append(reader.events, []byte(fmt.Sprintf(`{"type":"delta","id":"%d"}`, i+1)))
	}
	source := NewWSSource(reader)
	events := make(chan SourceEvent, 50)
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				event, err := source.ReadSourceEvent(context.Background())
				if err != nil {
					return
				}
				events <- event
			}
		}()
	}
	readers.Wait()
	close(events)
	seen := make(map[int64]bool)
	for event := range events {
		if event.ID != fmt.Sprint(event.Sequence) || seen[event.Sequence] {
			t.Fatalf("metadata ordering = %+v", event)
		}
		seen[event.Sequence] = true
	}
	if len(seen) != 50 {
		t.Fatalf("lost events: %d", len(seen))
	}
}

type contractDataErrorReader struct{ err error }

func (r contractDataErrorReader) Read(p []byte) (int, error) { return copy(p, "tail"), r.err }
func (r contractDataErrorReader) Close() error               { return nil }

func TestSourceContractRawRetainsErrorAfterData(t *testing.T) {
	failure := errors.New("source interrupted")
	source := NewRawSource(contractDataErrorReader{err: failure}, 16)
	first, err := source.ReadSourceEvent(context.Background())
	if err != nil || string(first.Data) != "tail" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := source.ReadSourceEvent(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("pending error lost: %v", err)
	}
}
