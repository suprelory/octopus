package op

import (
	"testing"
	"time"
)

func resetRelayLogStreamTokens() {
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens = make(map[string]time.Time)
	relayLogStreamTokensLock.Unlock()
}

// relayLogStreamTokenCount 断言 map 是否随过期收缩。
func relayLogStreamTokenCount() int {
	relayLogStreamTokensLock.Lock()
	defer relayLogStreamTokensLock.Unlock()
	return len(relayLogStreamTokens)
}

func TestRelayLogStreamTokenVerify(t *testing.T) {
	resetRelayLogStreamTokens()
	t.Cleanup(resetRelayLogStreamTokens)

	token, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatalf("RelayLogStreamTokenCreate: %v", err)
	}
	if !RelayLogStreamTokenVerify(token) {
		t.Fatal("a freshly issued token should verify")
	}
	if RelayLogStreamTokenVerify("not-a-real-token") {
		t.Fatal("an unknown token must not verify")
	}
}

func TestRelayLogStreamTokenRevoke(t *testing.T) {
	resetRelayLogStreamTokens()
	t.Cleanup(resetRelayLogStreamTokens)

	token, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatalf("RelayLogStreamTokenCreate: %v", err)
	}
	RelayLogStreamTokenRevoke(token)

	if RelayLogStreamTokenVerify(token) {
		t.Fatal("a revoked token must not verify")
	}
	if got := relayLogStreamTokenCount(); got != 0 {
		t.Fatalf("token map size after revoke = %d, want 0", got)
	}
}

// 前端 useLogStream 每次重连都会重新取一个 token，SSE 反复失败时每轮退避都签发
// 一个。没有 TTL 时这些 token 既泄漏内存又长期可用。
func TestRelayLogStreamTokenExpires(t *testing.T) {
	resetRelayLogStreamTokens()
	t.Cleanup(resetRelayLogStreamTokens)

	expired, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatalf("RelayLogStreamTokenCreate: %v", err)
	}

	// 把签发时间往前挪，避免测试真的等 30 秒。
	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens[expired] = time.Now().Add(-relayLogStreamTokenTTL - time.Second)
	relayLogStreamTokensLock.Unlock()

	if RelayLogStreamTokenVerify(expired) {
		t.Fatal("a token past its TTL must not verify")
	}
	if got := relayLogStreamTokenCount(); got != 0 {
		t.Fatalf("verifying an expired token should drop it; map size = %d, want 0", got)
	}
}

func TestRelayLogStreamTokenCreatePrunesExpired(t *testing.T) {
	resetRelayLogStreamTokens()
	t.Cleanup(resetRelayLogStreamTokens)

	// 模拟「反复重连但从不连上」：签发若干 token 后全部标记为过期。
	for i := 0; i < 5; i++ {
		token, err := RelayLogStreamTokenCreate()
		if err != nil {
			t.Fatalf("RelayLogStreamTokenCreate: %v", err)
		}
		relayLogStreamTokensLock.Lock()
		relayLogStreamTokens[token] = time.Now().Add(-relayLogStreamTokenTTL - time.Second)
		relayLogStreamTokensLock.Unlock()
	}

	fresh, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatalf("RelayLogStreamTokenCreate: %v", err)
	}

	if got := relayLogStreamTokenCount(); got != 1 {
		t.Fatalf("map size after prune = %d, want 1 (only the fresh token)", got)
	}
	if !RelayLogStreamTokenVerify(fresh) {
		t.Fatal("the freshly issued token should still verify after pruning")
	}
}
