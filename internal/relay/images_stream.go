package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

// proxySSE 将上游 SSE 逐行解析 event/data/空行并透传到下游；首事件计为 FirstTokenTime；支持 FirstTokenTimeOut 切换。
func proxySSE(ctx context.Context, c *gin.Context, respUp *http.Response, firstTokenTimeOutSec int, metrics *imagesRelayMetrics, hb *earlyHeartbeat) (*imagesUsage, bool, error) {
	if ct := respUp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		b, _ := io.ReadAll(io.LimitReader(respUp.Body, imagesUpstreamErrorBodyLimit))
		return nil, false, fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(b))
	}

	// 交接早期心跳给本函数内层 ticker
	hb.Hand()

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	heartbeatTicker, heartbeatC := newStreamHeartbeatTicker()
	if heartbeatTicker != nil {
		defer heartbeatTicker.Stop()
	}

	type lineResult struct {
		line []byte
		err  error
		eof  bool
	}

	results := make(chan lineResult, 1)
	go func() {
		defer close(results)
		br := bufio.NewReaderSize(respUp.Body, 64*1024)
		for {
			line, err := readLineLimited(br, maxSSEEventSize)
			if err != nil {
				if errors.Is(err, io.EOF) {
					results <- lineResult{eof: true}
					return
				}
				results <- lineResult{err: err}
				return
			}
			results <- lineResult{line: line}
		}
	}()

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	var (
		firstWrite       = true
		currentEvent     string
		completedScanner = newUsageScanner()
	)

	for {
		select {
		case <-ctx.Done():
			log.Infof("client disconnected, stopping stream")
			return completedScanner.Usage(), !firstWrite, nil

		case <-firstTokenC:
			log.Warnf("first token timeout (%ds), switching channel", firstTokenTimeOutSec)
			_ = respUp.Body.Close()
			return completedScanner.Usage(), !firstWrite, fmt.Errorf("first token timeout (%ds)", firstTokenTimeOutSec)

		case <-heartbeatC:
			if err := writeSSEHeartbeat(c.Writer); err != nil {
				return completedScanner.Usage(), false, err
			}

		case r, ok := <-results:
			if !ok {
				usage, written := completedScanner.Usage(), !firstWrite
				// Empty stream detection: no data written
				if !written {
					return usage, written, fmt.Errorf("empty image stream: no events received")
				}
				return usage, written, nil
			}
			if r.eof {
				usage, written := completedScanner.Usage(), !firstWrite
				// Empty stream detection: no data written
				if !written {
					return usage, written, fmt.Errorf("empty image stream: no events received")
				}
				return usage, written, nil
			}
			if r.err != nil {
				return completedScanner.Usage(), !firstWrite, fmt.Errorf("failed to read stream line: %w", r.err)
			}

			line := r.line
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) == 0 {
				// 空行：事件边界
				currentEvent = ""
			} else if bytes.HasPrefix(trimmed, []byte("event:")) {
				currentEvent = strings.TrimSpace(string(trimmed[len("event:"):]))
			} else if bytes.HasPrefix(trimmed, []byte("data:")) {
				// 仅在 completed 事件上尝试提取 usage（避免解析/分配巨大 b64_json）
				payload := bytes.TrimSpace(trimmed[len("data:"):])
				if currentEvent == "image_generation.completed" || bytes.Contains(payload, []byte(`"type":"image_generation.completed"`)) {
					completedScanner.Feed(payload)
				}
			}

			if _, werr := c.Writer.Write(line); werr != nil {
				return completedScanner.Usage(), true, werr
			}
			c.Writer.Flush()

			if firstWrite {
				metrics.SetFirstTokenTime(time.Now())
				firstWrite = false
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}
		}
	}
}

func readLineLimited(br *bufio.Reader, limit int) ([]byte, error) {
	var out []byte
	for {
		part, err := br.ReadSlice('\n')
		out = append(out, part...)
		if len(out) > limit {
			return nil, fmt.Errorf("sse line exceeds limit %d bytes", limit)
		}
		if err == nil {
			return out, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		// 允许返回已读部分 + err（调用方按 err 处理）
		return out, err
	}
}

type usageScanner struct {
	matchIdx       int
	waitForObject  bool
	collecting     bool
	braceDepth     int
	inString       bool
	escape         bool
	buf            bytes.Buffer
	usage          *imagesUsage
	done           bool
	maxCollectSize int
}

func newUsageScanner() *usageScanner {
	return &usageScanner{maxCollectSize: 64 * 1024}
}

// Feed 逐字节扫描输入，定位 "usage":{...} 并仅解析 usage 子对象。
// 该实现用于避免整体 json.Unmarshal 造成 b64_json 巨大内存分配。
func (s *usageScanner) Feed(p []byte) {
	if s.done || len(p) == 0 {
		return
	}
	const pat = `"usage":`

	for _, b := range p {
		if s.done {
			return
		}

		if s.collecting {
			if s.buf.Len() >= s.maxCollectSize {
				s.collecting = false
				s.done = true
				return
			}
			s.buf.WriteByte(b)

			if s.inString {
				if s.escape {
					s.escape = false
				} else if b == '\\' {
					s.escape = true
				} else if b == '"' {
					s.inString = false
				}
				continue
			}

			if b == '"' {
				s.inString = true
				continue
			}

			switch b {
			case '{':
				s.braceDepth++
			case '}':
				s.braceDepth--
				if s.braceDepth == 0 {
					var u imagesUsage
					if err := json.Unmarshal(s.buf.Bytes(), &u); err == nil {
						s.usage = &u
					}
					s.done = true
					s.collecting = false
					return
				}
			}
			continue
		}

		if s.waitForObject {
			if b == '{' {
				s.collecting = true
				s.braceDepth = 1
				s.buf.Reset()
				s.buf.WriteByte('{')
				s.inString = false
				s.escape = false
				s.waitForObject = false
				continue
			}
			// 跳过空白，遇到其他字符则放弃
			if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
				continue
			}
			s.waitForObject = false
			continue
		}

		// 匹配 "usage":
		if b == pat[s.matchIdx] {
			s.matchIdx++
			if s.matchIdx == len(pat) {
				s.waitForObject = true
				s.matchIdx = 0
			}
			continue
		}

		// 失败回退：若当前字符可能是 pat[0]，则 matchIdx=1
		if b == pat[0] {
			s.matchIdx = 1
		} else {
			s.matchIdx = 0
		}
	}
}

func (s *usageScanner) Usage() *imagesUsage {
	return s.usage
}
