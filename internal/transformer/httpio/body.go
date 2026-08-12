// Package httpio holds the small I/O helpers shared by relay and the outbound
// protocol adapters. It deliberately depends on nothing but the standard
// library so every adapter can import it.
package httpio

import (
	"fmt"
	"io"
)

// MaxResponseBodySize 是上游非流式响应体的默认上限。上游是用户自己配置的
// 半可信端点，坏掉或恶意的上游不应该能靠一个超大响应把网关拖垮。
//
// 64 MiB 对正常的 completion / embedding 响应绰绰有余（最大的现实场景是带
// base64 图片的多模态响应），同时把单次响应的内存占用限制在可控范围。
const MaxResponseBodySize = 64 << 20

// BodyTooLargeError 表示读取时超过了限制。relay 侧据此触发 failover，而不是
// 把截断的 JSON 当成正常响应去解析。
type BodyTooLargeError struct {
	Limit int64
}

func (e *BodyTooLargeError) Error() string {
	return fmt.Sprintf("body exceeds the %d byte limit", e.Limit)
}

// ReadBodyLimited 最多读取 limit 字节，超过则返回 *BodyTooLargeError。
// limit <= 0 表示不限制。
func ReadBodyLimited(r io.Reader, limit int64) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if limit <= 0 {
		return io.ReadAll(r)
	}

	// 多读 1 字节，用来区分「正好等于上限」和「超过上限」。
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &BodyTooLargeError{Limit: limit}
	}
	return body, nil
}

// ReadResponseBody 用 MaxResponseBodySize 读取上游响应体。
func ReadResponseBody(r io.Reader) ([]byte, error) {
	return ReadBodyLimited(r, MaxResponseBodySize)
}
