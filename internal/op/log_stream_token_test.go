package op

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func resetRelayLogStreamTokens() {
	relayLogStreamsLock.Lock()
	relayLogStreamTokens = make(map[string]relayLogStreamToken)
	relayLogStreamsLock.Unlock()
}

func setupLogStreamTokenTest(t *testing.T) {
	t.Helper()
	previous := UserGet()
	setUserCache(model.User{ID: 1, Username: "admin", Password: "synthetic-hash"})
	resetRelayLogStreamTokens()
	t.Cleanup(func() {
		resetRelayLogStreamTokens()
		setUserCache(previous)
	})
}

// relayLogStreamTokenCount 断言 map 是否随过期收缩。
func relayLogStreamTokenCount() int {
	relayLogStreamsLock.Lock()
	defer relayLogStreamsLock.Unlock()
	return len(relayLogStreamTokens)
}

func TestRelayLogStreamTokenVerify(t *testing.T) {
	setupLogStreamTokenTest(t)

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
	setupLogStreamTokenTest(t)

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
	setupLogStreamTokenTest(t)

	expired, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatalf("RelayLogStreamTokenCreate: %v", err)
	}

	// 把签发时间往前挪，避免测试真的等 30 秒。
	relayLogStreamsLock.Lock()
	entry := relayLogStreamTokens[expired]
	entry.issuedAt = time.Now().Add(-relayLogStreamTokenTTL - time.Second)
	relayLogStreamTokens[expired] = entry
	relayLogStreamsLock.Unlock()

	if RelayLogStreamTokenVerify(expired) {
		t.Fatal("a token past its TTL must not verify")
	}
	if got := relayLogStreamTokenCount(); got != 0 {
		t.Fatalf("verifying an expired token should drop it; map size = %d, want 0", got)
	}
}

func TestRelayLogStreamTokenCreatePrunesExpired(t *testing.T) {
	setupLogStreamTokenTest(t)

	// 模拟「反复重连但从不连上」：签发若干 token 后全部标记为过期。
	for i := 0; i < 5; i++ {
		token, err := RelayLogStreamTokenCreate()
		if err != nil {
			t.Fatalf("RelayLogStreamTokenCreate: %v", err)
		}
		relayLogStreamsLock.Lock()
		entry := relayLogStreamTokens[token]
		entry.issuedAt = time.Now().Add(-relayLogStreamTokenTTL - time.Second)
		relayLogStreamTokens[token] = entry
		relayLogStreamsLock.Unlock()
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

func TestRelayLogStreamTokenConsumedOnce(t *testing.T) {
	setupLogStreamTokenTest(t)
	token, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatal(err)
	}
	const clients = 16
	start := make(chan struct{})
	results := make(chan *RelayLogSubscription, clients)
	var wg sync.WaitGroup
	for range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			sub, _ := RelayLogSubscribeWithToken(token)
			results <- sub
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for sub := range results {
		if sub != nil {
			accepted++
			sub.Close()
			sub.Close() // Disconnect and revocation may both perform cleanup.
		}
	}
	if accepted != 1 || RelayLogStreamTokenVerify(token) {
		t.Fatalf("single-use token admitted %d clients", accepted)
	}
}

func TestPasswordChangeRevokesLogStreamGeneration(t *testing.T) {
	setupSiteOpTestDB(t)
	initTestUser(t)
	oldUser := UserGet()
	unused, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatal(err)
	}
	activeToken, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := RelayLogSubscribeWithToken(activeToken)
	if !ok {
		t.Fatal("fresh token was rejected")
	}
	defer sub.Close()
	if err := UserChangePassword("wrong-password", "replacement-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if err := UserChangeUsername("renamed-admin"); err != nil {
		t.Fatal(err)
	}
	if !RelayLogStreamTokenVerify(unused) {
		t.Fatal("failed password change or rename revoked the token")
	}
	select {
	case <-sub.Done():
		t.Fatal("failed password change or rename revoked the subscription")
	default:
	}
	if err := UserChangePassword(testAdminPassword, "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if RelayLogStreamTokenVerify(unused) {
		t.Fatal("unused token survived password change")
	}
	if stale, ok := RelayLogSubscribeWithToken(unused); ok {
		stale.Close()
		t.Fatal("old token opened a subscription")
	}
	if _, err := RelayLogStreamTokenCreateForUser(oldUser); !errors.Is(err, ErrRelayLogSessionExpired) {
		t.Fatalf("stale authenticated snapshot minted a token: %v", err)
	}
	select {
	case <-sub.Done():
	default:
		t.Fatal("password change left subscription open")
	}
	notifySubscribers(model.RelayLog{ID: 123})
	if _, ok := <-sub.Logs(); ok {
		t.Fatal("revoked subscriber received a later log")
	}
	fresh, err := RelayLogStreamTokenCreate()
	if err != nil {
		t.Fatal(err)
	}
	newSub, ok := RelayLogSubscribeWithToken(fresh)
	if !ok {
		t.Fatal("new credentials cannot subscribe")
	}
	newSub.Close()
}
