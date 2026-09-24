package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestKeyFailuresRotateWithinChannelAndRespectBudget(t *testing.T) {
	for _, operation := range []string{"text", "images", "compact"} {
		for _, limited := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/rotate", true: "/budget"}[limited], func(t *testing.T) {
				ctx := setupHTTPRelayTestDB(t)
				if limited {
					if err := op.SettingSetInt(dbmodel.SettingKeyRelayMaxTotalAttempts, 1); err != nil {
						t.Fatal(err)
					}
				}
				var badHits, goodHits atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") == "Bearer key-0" {
						badHits.Add(1)
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key","message":"invalid api key"}}`)
						return
					}
					goodHits.Add(1)
					w.Header().Set("Content-Type", "application/json")
					if operation == "images" {
						_, _ = io.WriteString(w, `{"data":[{"url":"image"}]}`)
					} else {
						_, _ = io.WriteString(w, relayTestResponseJSON("resp_ok", "answer"))
					}
				}))
				defer server.Close()
				group := &dbmodel.Group{Name: "key-rotation", Mode: dbmodel.GroupModeFailover, RetryEnabled: true, MaxRetries: 3}
				channels := addHTTPRelayTestChannels(t, ctx, group, dbmodel.ChannelPassthroughModeOff, server.URL)
				update := &dbmodel.ChannelUpdateRequest{ID: channels[0].ID, KeysToAdd: []dbmodel.ChannelKeyAddRequest{{Enabled: true, ChannelKey: "good"}}}
				if operation == "images" {
					typ := outbound.OutboundTypeOpenAIChat
					update.Type = &typ
				}
				channel, err := op.ChannelUpdate(update, ctx)
				if err != nil {
					t.Fatal(err)
				}
				balancer.SetRoutingAffinity(7, group.ID, group.Name, channel.ID, channels[0].Keys[0].ID)
				c, recorder := newHTTPRelayTestContext(ctx, `{"model":"key-rotation","input":"hello","prompt":"hello"}`)
				switch operation {
				case "images":
					ImagesHandler("/images/generations", c)
				case "compact":
					HandleResponsesCompact(c)
				default:
					Handler(inbound.InboundTypeOpenAIResponse, c)
				}
				wantGood, wantStatus := int32(1), http.StatusOK
				if limited {
					wantGood, wantStatus = 0, http.StatusGatewayTimeout
				}
				if badHits.Load() != 1 || goodHits.Load() != wantGood || recorder.Code != wantStatus {
					t.Fatalf("bad=%d good=%d status=%d body=%s", badHits.Load(), goodHits.Load(), recorder.Code, recorder.Body.String())
				}
				if balancer.CanAttempt(channel.ID, channels[0].Keys[0].ID, "other-model") {
					t.Fatal("bad key was not isolated across models")
				}
				logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
				if err != nil || len(logs) != 1 || logs[0].Attempts[0].FailureScope != "key" {
					t.Fatalf("logs=%+v err=%v", logs, err)
				}
				assertHTTPRelayKeysReleased(t, []*dbmodel.Channel{channel})
			})
		}
	}
}

func TestFailureScopesDistinguishIndependentQuota(t *testing.T) {
	for _, tc := range []struct{ message, scope string }{
		{"api_key_quota_exceeded", "key"}, {"insufficient_quota: account billing hard limit", "channel"},
		{"key_rate_limit_exceeded", "key"}, {"rate_limit_exceeded", "channel"}, {"model_not_found", "model"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			failure := classifyRelayFailure(429, relayProtocolError(429, tc.message, tc.message), time.Time{})
			// A model error must be explicit rather than a generic 429.
			if tc.scope == "model" {
				failure = classifyRelayFailure(400, relayProtocolError(400, tc.message, tc.message), time.Time{})
			}
			if failure.Scope != tc.scope {
				t.Fatalf("failure=%+v", failure)
			}
			result := attemptResult{Failure: failure}
			if mayRotateKey(result, true) {
				t.Fatal("native continuation must not rotate keys")
			}
			result.Written = true
			if mayRotateKey(result, false) {
				t.Fatal("committed response must not rotate keys")
			}
		})
	}
}
