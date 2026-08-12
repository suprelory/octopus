package relay

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"
)

// isRetryableStatus 判断 HTTP 状态码是否可重试
// 429(限流)、503(服务不可用)、>=500(服务端错误)、0(连接错误) 可重试
// 400/401/403/404 等客户端错误不可重试
func isRetryableStatus(code int) bool {
	return code == 0 || code == 429 || code >= 500
}

// isPassthroughStatus 判断是否应透传给下游客户端
// 429 和 503 透传，让客户端 SDK 的重试机制接管
func isPassthroughStatus(code int) bool {
	return code == 429 || code == 503
}

// parseRetryAfter 解析 Retry-After 响应头（仅支持秒数格式），上限 60s
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	secs, err := strconv.Atoi(header)
	if err != nil || secs <= 0 {
		return 0
	}
	d := time.Duration(secs) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
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
