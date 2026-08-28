package snowflake

import (
	"fmt"
	"math"
	"sync"
	"time"
)

var (
	sfMutex    sync.Mutex
	sfLastTime int64
)

// GenerateID 生成唯一ID
// 基于毫秒时间戳，当同一毫秒内调用时等待到下一毫秒
func GenerateID() int64 {
	sfMutex.Lock()
	defer sfMutex.Unlock()

	now := time.Now().UnixMilli()

	if now <= sfLastTime {
		sfLastTime++
		return sfLastTime
	}

	sfLastTime = now
	return now
}

// GenerateIDAfter returns an ID strictly greater than floor while advancing the
// process-local generator. It is used when persisted IDs may have been imported
// from another process or generated before a wall-clock rollback.
func GenerateIDAfter(floor int64) (int64, error) {
	sfMutex.Lock()
	defer sfMutex.Unlock()

	if floor == math.MaxInt64 || sfLastTime == math.MaxInt64 {
		return 0, fmt.Errorf("cannot generate an ID after %d", floor)
	}

	next := time.Now().UnixMilli()
	if next <= sfLastTime {
		next = sfLastTime + 1
	}
	if next <= floor {
		next = floor + 1
	}

	sfLastTime = next
	return next, nil
}
