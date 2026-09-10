package stream

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func BenchmarkSSEContractDecode(b *testing.B) {
	for _, test := range []struct {
		name          string
		data          string
		frames, chunk int
	}{
		{"SmallChunks", `{"type":"response.output_text.delta","delta":"x"}`, 64, 7},
		{"LargeImage", `{"type":"response.image_generation_call.partial_image","partial_image_b64":"` + strings.Repeat("A", 1<<20) + `"}`, 1, 4096},
		{"ToolArguments", `{"type":"response.function_call_arguments.delta","output_index":1,"delta":"\"query\":\"value\""}`, 256, 256},
		{"HighFrequencyText", `{"type":"response.output_text.delta","delta":"token"}`, 1024, 4096},
	} {
		wire := strings.Repeat("id: event\nevent: delta\ndata: "+test.data+"\n\n", test.frames)
		for _, mode := range []string{"Source", "Observer"} {
			b.Run(test.name+"/"+mode, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(wire)))
				ctx := context.Background()
				chunk := []byte(wire)
				b.ResetTimer()
				for range b.N {
					count := 0
					if mode == "Source" {
						source := NewSSESource(io.NopCloser(contractChunkReader{strings.NewReader(wire), test.chunk}), 0)
						for {
							_, err := source.ReadSourceEvent(ctx)
							if errors.Is(err, io.EOF) {
								break
							}
							if err != nil {
								b.Fatal(err)
							}
							count++
						}
						if err := source.Close(); err != nil {
							b.Fatal(err)
						}
					} else {
						observer := NewIncrementalSourceEventObserver(0, nil, func(context.Context, SourceEvent) error { count++; return nil })
						for offset := 0; offset < len(chunk); offset += test.chunk {
							if err := observer.Observe(ctx, chunk[offset:min(offset+test.chunk, len(chunk))]); err != nil {
								b.Fatal(err)
							}
						}
						if err := observer.Finalize(ctx); err != nil {
							b.Fatal(err)
						}
					}
					if count != test.frames {
						b.Fatalf("decoded %d frames, want %d", count, test.frames)
					}
				}
			})
		}
	}
}
