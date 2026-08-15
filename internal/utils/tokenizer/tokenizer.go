package tokenizer

import (
	"runtime"
	"unsafe"

	"github.com/tiktoken-go/tokenizer/codec"
)

// NewO200kBase 每次调用都会重新编译约 600 字符的 Unicode BPE 切分正则
// （sync.Once 只保护词表），而 CountTokens 在 inbound 转换中被逐 system
// block、逐 message、逐 content block、逐 tool 调用，且每次重试都会重跑
// TransformRequest，因此这里将 codec 缓存为包级单例。
// regexp2.Regexp 文档保证并发安全；Count 只读词表 map。
// 注意：Codec.Decode 会惰性写入 reverseVocabulary（非并发安全），
// 本包只用 Count；若将来需要 Decode，须另行加锁。
var enc = codec.NewO200kBase()

func CountTokens(content, model string) int {
	return countTokens(content, model)
}

// CountTokensBytes counts a read-only payload without allocating a temporary
// string copy. codec.Count consumes the string synchronously and does not retain
// it; callers must not mutate content concurrently with this call.
func CountTokensBytes(content []byte, model string) int {
	if len(content) == 0 {
		return 0
	}
	view := unsafe.String(unsafe.SliceData(content), len(content))
	count := countTokens(view, model)
	runtime.KeepAlive(content)
	return count
}

func countTokens(content, model string) int {
	// TODO 更多模型
	tc, err := enc.Count(content)
	if err != nil {
		return 0
	}
	return tc
}
