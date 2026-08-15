package tokenizer

import (
	"strings"
	"sync"
	"testing"
)

func TestCountTokensBasic(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"ascii word", "hello", 1},
		{"sentence", "hello world", 2},
		{"chinese", "你好世界", 2}, // o200k_base 中“你好”“世界”各为单个合并 token
		{"mixed", "Hello, 世界! 123", 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountTokens(tt.content, "claude-sonnet-4-5"); got != tt.want {
				t.Errorf("CountTokens(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

func TestCountTokensBytesMatchesString(t *testing.T) {
	tests := []string{
		"",
		"hello world",
		"你好，世界",
		`{"model":"gpt-4o-mini","input":"hello world"}`,
		string([]byte{0xff, 'a', 0xfe}),
	}
	for _, content := range tests {
		if got, want := CountTokensBytes([]byte(content), "test"), CountTokens(content, "test"); got != want {
			t.Fatalf("CountTokensBytes(%q) = %d, want %d", content, got, want)
		}
	}
}

// CountTokens 现在共享一个包级 codec（regexp2.Regexp 并发安全），
// 用 -race 验证多 goroutine 调用无数据竞争。
func TestCountTokensConcurrent(t *testing.T) {
	text := strings.Repeat("Analyze the following system prompt and tool definitions. ", 20)
	payload := []byte(text)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if CountTokens(text, "test") <= 0 || CountTokensBytes(payload, "test") <= 0 {
					t.Error("token counter returned non-positive count for non-empty text")
					return
				}
			}
		}()
	}
	wg.Wait()
}
