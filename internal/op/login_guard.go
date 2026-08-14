package op

import (
	"sync"
	"time"
)

// 登录接口的暴力破解防护。与 RateLimitCheck（按 API Key 限流）分开，因为登录
// 发生在认证之前，只能按来源限流，而且需要对连续失败做指数退避。
const (
	// loginMaxRPM 是单个来源每分钟允许的登录尝试次数。
	loginMaxRPM = 10
	// loginFailureThreshold 是触发退避前容忍的连续失败次数，留给正常手滑。
	loginFailureThreshold = 5
	// loginBackoffBase / loginBackoffMax 是退避的起点与封顶。
	loginBackoffBase = 2 * time.Second
	loginBackoffMax  = 15 * time.Minute
	// loginBackoffMaxShift 限制移位次数，避免 Duration 溢出成负数。
	loginBackoffMaxShift = 20
	// loginGuardIdleTTL 回收「只是来登录过、没有失败记录」的条目。这类条目只需
	// 要活过 1 分钟的 RPM 窗口，留久了会被大量一次性来源撑满表。
	loginGuardIdleTTL = 2 * time.Minute
	// loginGuardFailedTTL 是有失败记录的条目的保留时长 —— 攻击者不该靠等一会儿
	// 就把自己的失败计数洗掉。
	loginGuardFailedTTL = 30 * time.Minute
	// loginGuardPruneInterval 控制回收频率，避免每次请求都遍历整张表。
	loginGuardPruneInterval = 5 * time.Minute
	// loginGuardMaxEntries 是条目数硬上限。达到上限且回收不动时对**新**来源
	// 直接拒绝（fail closed）——登录端点宁可短暂拒绝，也不要被伪造来源撑爆内存。
	loginGuardMaxEntries = 10000
)

type loginGuardEntry struct {
	minute      int64
	count       int64
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

var (
	loginGuardLock      sync.Mutex
	loginGuard          = make(map[string]*loginGuardEntry)
	loginGuardLastPrune time.Time
)

// LoginAttemptAllow 判断 source 是否还能再尝试登录。
// 返回 (false, retryAfterSeconds) 时调用方应当返回 429。
//
// source 通常由 server/middleware.ClientIP 解析。默认不信任任何代理，此时
// 等于 TCP 对端地址，无法被 X-Forwarded-For 伪造。
func LoginAttemptAllow(source string) (bool, int) {
	if source == "" {
		return true, 0
	}
	now := time.Now()

	loginGuardLock.Lock()
	defer loginGuardLock.Unlock()

	loginGuardPruneLocked(now)

	entry, ok := loginGuard[source]
	if !ok {
		if len(loginGuard) >= loginGuardMaxEntries {
			return false, retryAfterSeconds(loginBackoffBase)
		}
		entry = &loginGuardEntry{}
		loginGuard[source] = entry
	}
	entry.lastSeen = now

	if now.Before(entry.lockedUntil) {
		return false, retryAfterSeconds(entry.lockedUntil.Sub(now))
	}

	currentMinute := now.Unix() / 60
	if entry.minute != currentMinute {
		entry.minute = currentMinute
		entry.count = 0
	}
	entry.count++
	if entry.count > loginMaxRPM {
		return false, retryAfterSeconds(time.Duration(60-now.Unix()%60) * time.Second)
	}

	return true, 0
}

// LoginAttemptFailed 记一次失败。超过阈值后每多失败一次，退避时间翻倍。
func LoginAttemptFailed(source string) {
	if source == "" {
		return
	}
	now := time.Now()

	loginGuardLock.Lock()
	defer loginGuardLock.Unlock()

	entry, ok := loginGuard[source]
	if !ok {
		if len(loginGuard) >= loginGuardMaxEntries {
			return
		}
		entry = &loginGuardEntry{}
		loginGuard[source] = entry
	}
	entry.lastSeen = now
	entry.failures++

	if entry.failures <= loginFailureThreshold {
		return
	}
	entry.lockedUntil = now.Add(loginBackoff(entry.failures))
}

// LoginAttemptSucceeded 清掉该来源的失败计数与退避。
func LoginAttemptSucceeded(source string) {
	if source == "" {
		return
	}
	loginGuardLock.Lock()
	delete(loginGuard, source)
	loginGuardLock.Unlock()
}

func loginBackoff(failures int) time.Duration {
	shift := failures - loginFailureThreshold - 1
	if shift < 0 {
		shift = 0
	}
	if shift > loginBackoffMaxShift {
		shift = loginBackoffMaxShift
	}
	backoff := loginBackoffBase << shift
	if backoff <= 0 || backoff > loginBackoffMax {
		return loginBackoffMax
	}
	return backoff
}

// loginGuardPruneLocked 回收过期条目。调用方必须持有 loginGuardLock。
// 仍在退避中的条目一律保留，否则攻击者可以靠拖时间把自己的锁定清掉。
func loginGuardPruneLocked(now time.Time) {
	if now.Sub(loginGuardLastPrune) < loginGuardPruneInterval &&
		len(loginGuard) < loginGuardMaxEntries {
		return
	}
	loginGuardLastPrune = now

	for source, entry := range loginGuard {
		if now.Before(entry.lockedUntil) {
			continue
		}
		if now.Sub(entry.lastSeen) > loginGuardEntryTTL(entry) {
			delete(loginGuard, source)
		}
	}
}

// loginGuardEntryTTL 决定一个条目该留多久：没有失败记录的短留，有失败记录的
// 长留。这样一批一次性来源不会把表占住 30 分钟，把正常用户也一起挡在外面。
func loginGuardEntryTTL(entry *loginGuardEntry) time.Duration {
	if entry.failures == 0 {
		return loginGuardIdleTTL
	}
	return loginGuardFailedTTL
}

func retryAfterSeconds(d time.Duration) int {
	seconds := int(d / time.Second)
	if d%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}
