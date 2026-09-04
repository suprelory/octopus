package relay

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRelayRetryWait limits how long one request may remain blocked by an
// upstream hint. The absolute deadline is still retained for the breaker and
// for the response sent to the client.
const maxRelayRetryWait = 10 * time.Minute

// isPassthroughStatus 判断是否应透传给下游客户端
// 429 和 503 透传，让客户端 SDK 的重试机制接管
func isPassthroughStatus(code int) bool {
	return code == 429 || code == 503 || code == 529
}

// parseRetryAt parses both Retry-After forms (delta-seconds and HTTP-date) and
// returns an absolute deadline. Invalid values return the zero time.
func parseRetryAt(header string) time.Time {
	return parseRetryAtAt(header, time.Now())
}

func parseRetryAtAt(header string, now time.Time) time.Time {
	header = strings.TrimSpace(header)
	if header == "" {
		return time.Time{}
	}
	if secs, err := strconv.ParseInt(header, 10, 64); err == nil {
		if secs < 0 {
			return time.Time{}
		}
		maxSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
		if secs > maxSeconds {
			return time.Time{}
		}
		return now.Add(time.Duration(secs) * time.Second)
	}
	if parsed, err := http.ParseTime(header); err == nil {
		return parsed
	}
	return time.Time{}
}

// computeBackoff 计算退避时间
// 优先使用 retryAfter（上游指定的等待时间），否则使用指数退避 + jitter
// retryNum 从 1 开始（第1次重试）
func computeBackoff(retryNum int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}

	// 指数退避: 1s * 2^(retryNum-1)
	base := time.Second
	shift := retryNum - 1
	if shift > 5 {
		shift = 5
	}
	delay := base << shift

	if delay > 60*time.Second {
		delay = 60 * time.Second
	}

	// 添加 10%-50% 的 jitter 防止惊群
	jitter := time.Duration(float64(delay) * (0.1 + rand.Float64()*0.4))
	return delay + jitter
}

// computeBackoffUntil uses the absolute deadline supplied by an upstream. A
// deadline in the past is ignored and falls back to local exponential backoff.
func computeBackoffUntil(retryNum int, retryAt time.Time) time.Duration {
	if !retryAt.IsZero() {
		delay := time.Until(retryAt)
		if delay > 0 {
			if delay > maxRelayRetryWait {
				return maxRelayRetryWait
			}
			return delay
		}
		// An explicit Retry-After: 0 (or an already elapsed HTTP date)
		// means the provider permits an immediate retry. Do not replace that
		// signal with a one-second local backoff.
		return 0
	}
	return computeBackoff(retryNum, 0)
}

func computeAttemptBackoff(retryNum int, retryAt time.Time, retryAfter time.Duration) time.Duration {
	if !retryAt.IsZero() {
		return computeBackoffUntil(retryNum, retryAt)
	}
	return computeBackoff(retryNum, retryAfter)
}

// retryAfterHeaderValue returns the client-facing delta in whole seconds,
// rounded up so the advertised deadline is never earlier than retryAt.
func retryAfterHeaderValue(retryAt time.Time, now time.Time) string {
	if retryAt.IsZero() {
		return ""
	}
	delay := retryAt.Sub(now)
	if delay <= 0 {
		return ""
	}
	seconds := int64(delay / time.Second)
	if delay%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}

func retryAfterDurationHeaderValue(delay time.Duration) string {
	if delay <= 0 {
		return ""
	}
	seconds := int64(delay / time.Second)
	if delay%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}

// waitBackoff 等待 delay 或 ctx 取消，返回 false 表示 ctx 已取消。
//
// 用 time.NewTimer + Stop 而不是 time.After：后者创建的 timer 在触发前不会被
// 回收，而这里的退避最长可以到 60s，高并发重试下会持续积累。
func waitBackoff(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
