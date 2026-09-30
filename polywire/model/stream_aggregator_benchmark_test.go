package model

import (
	"fmt"
	"strings"
	"testing"
)

var aggregateBenchmarkResult *InternalLLMResponse

func BenchmarkStreamAggregator(b *testing.B) {
	for _, kind := range []string{"text", "reasoning", "tool_arguments", "native_text"} {
		for _, count := range []int{128, 2048} {
			b.Run(fmt.Sprintf("%s/chunks=%d", kind, count), func(b *testing.B) {
				fragment := strings.Repeat("x", 32)
				index := 0
				delta := &Message{}
				switch kind {
				case "text":
					delta.Content.Content = &fragment
				case "reasoning":
					delta.ReasoningContent = &fragment
				case "tool_arguments":
					delta.ToolCalls = []ToolCall{{Index: 0, Function: FunctionCall{Arguments: fragment}}}
				case "native_text":
					delta.Content.MultipleContent = []MessageContentPart{{Type: "text", BlockIndex: &index, Text: &fragment}}
				}
				chunk := &InternalLLMResponse{Choices: []Choice{{Index: 0, Delta: delta}}}
				b.ReportAllocs()
				b.SetBytes(int64(count * len(fragment)))
				b.ResetTimer()
				for range b.N {
					var aggregate StreamAggregator
					for range count {
						aggregate.Add(chunk)
					}
					aggregateBenchmarkResult = aggregate.BuildAndReset()
				}
			})
		}
	}
}
