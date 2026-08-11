package model

import (
	"strings"
	"testing"
)

func BenchmarkInternalRequestClone(b *testing.B) {
	for _, size := range []int{1024, 1 << 20} {
		b.Run(strings.ReplaceAll(byteSizeName(size), " ", ""), func(b *testing.B) {
			text := strings.Repeat("x", size)
			request := &InternalLLMRequest{
				Model: "m",
				Operation: &RequestOperation{Responses: &ResponsesOperation{Messages: []Message{{
					Role:    "user",
					Content: MessageContent{Content: &text},
				}}}},
				TransformerMetadata: map[string]string{"mode": "benchmark"},
			}
			if err := request.Validate(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for range b.N {
				if clone := request.Clone(); clone == nil {
					b.Fatal("nil clone")
				}
			}
		})
	}
}

func byteSizeName(size int) string {
	if size >= 1<<20 {
		return "1MiB"
	}
	return "1KiB"
}
