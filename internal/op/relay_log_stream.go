package op

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

var relayLogSubscribers = make(map[chan model.RelayLog]struct{})

var relayLogSubscribersLock sync.RWMutex

// relayLogStreamTokenTTL 是 /log/stream-token 的有效期。前端 useLogStream 每次
// 重连都会重新取一个 token，SSE 反复失败时每轮退避都会签发一个；没有 TTL 时
// 这些 token 既是内存泄漏，也长期可用。签发和校验都顺手清理过期项。
const relayLogStreamTokenTTL = 30 * time.Second

// relayLogStreamTokens 记录 token 的签发时间。
var relayLogStreamTokens = make(map[string]time.Time)

var relayLogStreamTokensLock sync.Mutex

func RelayLogStreamTokenCreate() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	now := time.Now()
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokensPruneLocked(now)
	relayLogStreamTokens[token] = now
	relayLogStreamTokensLock.Unlock()

	return token, nil
}

func RelayLogStreamTokenVerify(token string) bool {
	now := time.Now()

	relayLogStreamTokensLock.Lock()
	defer relayLogStreamTokensLock.Unlock()

	issuedAt, ok := relayLogStreamTokens[token]
	if !ok {
		return false
	}
	if now.Sub(issuedAt) > relayLogStreamTokenTTL {
		delete(relayLogStreamTokens, token)
		return false
	}
	return true
}

func RelayLogStreamTokenRevoke(token string) {
	relayLogStreamTokensLock.Lock()
	delete(relayLogStreamTokens, token)
	relayLogStreamTokensLock.Unlock()
}

// relayLogStreamTokensPruneLocked 删除已过期的 token。调用方必须持有锁。
func relayLogStreamTokensPruneLocked(now time.Time) {
	for token, issuedAt := range relayLogStreamTokens {
		if now.Sub(issuedAt) > relayLogStreamTokenTTL {
			delete(relayLogStreamTokens, token)
		}
	}
}

func RelayLogSubscribe() chan model.RelayLog {
	ch := make(chan model.RelayLog, 10)
	relayLogSubscribersLock.Lock()
	relayLogSubscribers[ch] = struct{}{}
	relayLogSubscribersLock.Unlock()
	return ch
}

func RelayLogUnsubscribe(ch chan model.RelayLog) {
	relayLogSubscribersLock.Lock()
	delete(relayLogSubscribers, ch)
	relayLogSubscribersLock.Unlock()
	close(ch)
}

func notifySubscribers(relayLog model.RelayLog) {
	relayLogSubscribersLock.RLock()
	defer relayLogSubscribersLock.RUnlock()

	for ch := range relayLogSubscribers {
		select {
		case ch <- relayLog:
		default:
		}
	}
}
