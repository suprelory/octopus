package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestFetchModelsUsesBrowserHeadersAndSummarizesHTMLError(t *testing.T) {
	observedUserAgent := ""
	observedAccept := ""
	observedAcceptLanguage := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUserAgent = r.Header.Get("User-Agent")
		observedAccept = r.Header.Get("Accept")
		observedAcceptLanguage = r.Header.Get("Accept-Language")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title></head><body>blocked</body></html>`))
	}))
	defer server.Close()

	_, err := FetchModels(context.Background(), model.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 0}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "managed-key"}},
	})
	if err == nil {
		t.Fatalf("expected FetchModels to fail")
	}
	if !strings.Contains(err.Error(), "http 403: Just a moment...") {
		t.Fatalf("expected summarized HTML error, got %v", err)
	}
	if !strings.Contains(observedUserAgent, "Mozilla/5.0") {
		t.Fatalf("expected browser user-agent, got %q", observedUserAgent)
	}
	if observedAccept == "" {
		t.Fatalf("expected Accept header to be set")
	}
	if observedAcceptLanguage == "" {
		t.Fatalf("expected Accept-Language header to be set")
	}
}

// hungModelServer serves requests that never respond until the test releases them.
func hungModelServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server
}

// The model fetch must impose its own deadline: the context here has none, and
// the shared relay client sets no Timeout, so an unresponsive upstream would
// otherwise hang the caller — site sync runs this once per candidate base URL.
func TestFetchModelsAppliesPerRequestTimeout(t *testing.T) {
	cases := []struct {
		name        string
		channelType outbound.OutboundType
	}{
		{name: "openai", channelType: outbound.OutboundTypeOpenAIChat},
		{name: "anthropic", channelType: outbound.OutboundTypeAnthropic},
		{name: "gemini", channelType: outbound.OutboundTypeGemini},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := hungModelServer(t)

			start := time.Now()
			_, err := fetchModels(context.Background(), model.Channel{
				Type:     testCase.channelType,
				BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 0}},
				Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "managed-key"}},
			}, 150*time.Millisecond)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("expected the hung request to time out")
			}
			if elapsed > 5*time.Second {
				t.Fatalf("request was not bounded by the per-request timeout (took %s)", elapsed)
			}
		})
	}
}

// A paginated listing gets the bound per page, so the deadline of an early page
// must not leak into later ones.
func TestFetchModelsBoundsEachPageSeparately(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		w.Header().Set("Content-Type", "application/json")
		if pages == 1 {
			// Spend most of one page's budget, then hand back a next page.
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(`{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"second"}],"has_more":false}`))
	}))
	defer server.Close()

	models, err := fetchModels(context.Background(), model.Channel{
		Type:     outbound.OutboundTypeAnthropic,
		BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 0}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "managed-key"}},
	}, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchModels returned error: %v", err)
	}
	if len(models) != 2 || models[0] != "first" || models[1] != "second" {
		t.Fatalf("expected both pages to be fetched, got %v", models)
	}
	if pages != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", pages)
	}
}
