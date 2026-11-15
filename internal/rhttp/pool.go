package rhttp

import (
	"container/list"
	"context"
	"net/http"
	"sync"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

const proxyClientLimit = 32

type cachedProxyClient struct {
	key    string
	client *http.Client
}

var poolClients = struct {
	sync.Mutex
	entries map[string]*list.Element
	lru     *list.List
}{entries: map[string]*list.Element{}, lru: list.New()}

func init() { op.RegisterProxyInvalidator(ClearProxyClients) }

func ClearProxyClients() {
	poolClients.Lock()
	defer poolClients.Unlock()
	for _, entry := range poolClients.entries {
		entry.Value.(cachedProxyClient).client.CloseIdleConnections()
	}
	poolClients.entries = map[string]*list.Element{}
	poolClients.lru.Init()
}

func ForSelection(ctx context.Context, selection model.ProxySelection) (*http.Client, error) {
	resolved, err := op.ResolveProxy(ctx, db.GetDB(), selection)
	if err != nil {
		return nil, err
	}
	return ForResolved(resolved)
}

// ForResolved returns a shared client. Callers must not mutate it or its transport.
// Check-in uses New with the same resolved endpoint to retain an exclusive transport.
func ForResolved(resolved model.ResolvedProxy) (*http.Client, error) {
	if resolved.URL == "" {
		return Direct()
	}
	u, err := model.ParseProxyURL(resolved.URL)
	if err != nil {
		return nil, err
	}
	key := u.String()
	poolClients.Lock()
	defer poolClients.Unlock()
	if entry, ok := poolClients.entries[key]; ok {
		poolClients.lru.MoveToFront(entry)
		return entry.Value.(cachedProxyClient).client, nil
	}
	client, err := New(key)
	if err != nil {
		return nil, err
	}
	entry := poolClients.lru.PushFront(cachedProxyClient{key, client})
	poolClients.entries[key] = entry
	if poolClients.lru.Len() > proxyClientLimit {
		old := poolClients.lru.Back()
		value := old.Value.(cachedProxyClient)
		delete(poolClients.entries, value.key)
		poolClients.lru.Remove(old)
		value.client.CloseIdleConnections()
	}
	return client, nil
}
