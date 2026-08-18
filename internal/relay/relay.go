package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/compat"
	"github.com/bestruirui/octopus/internal/transformer/httpio"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

// maxUpstreamErrorBodySize bounds provider-controlled error responses. Some
// providers echo the entire request in validation errors, which can otherwise
// turn a single failed request into an unbounded allocation and oversized log.
const maxUpstreamErrorBodySize = 1 << 20 // 1 MiB

func readUpstreamErrorBody(reader io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(reader, maxUpstreamErrorBodySize))
}

type streamHeartbeatWriter interface {
	Write([]byte) (int, error)
	Flush()
}

func streamHeartbeatInterval() time.Duration {
	interval, err := op.SettingGetInt(dbmodel.SettingKeySSEHeartbeatInterval)
	if err != nil || interval <= 0 {
		return 0
	}
	return time.Duration(interval) * time.Second
}

func newStreamHeartbeatTicker() (*time.Ticker, <-chan time.Time) {
	interval := streamHeartbeatInterval()
	if interval <= 0 {
		return nil, nil
	}
	ticker := time.NewTicker(interval)
	return ticker, ticker.C
}

func writeSSEHeartbeat(writer streamHeartbeatWriter) error {
	if _, err := writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	writer.Flush()
	return nil
}

func Handler(inboundType inbound.InboundType, c *gin.Context) {
	// 解析请求
	rawBody, internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			writeInboundProtocolError(c, nil, inAdapter, relayProtocolError(http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported"))
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")
	emptyResponseDetection := emptyResponseDetectionEnabled()

	// 获取通道分组
	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		writeInboundProtocolError(c, nil, inAdapter, relayProtocolError(http.StatusNotFound, CodeRelayModelNotFound, "model not found"))
		return
	}
	candidateSnapshot := newCandidateSnapshot(c.Request.Context(), group)

	// === HTTP Replay 机制 ===
	// 当 HTTP 请求携带 previous_response_id 时，尝试从本地加载上一次成功的 replay 状态，
	// 优先路由到同一渠道/key，并将请求转为自包含形式（合并历史，移除 previous_response_id）。
	var responsesReplayState *wsConversationState
	if inboundType == inbound.InboundTypeOpenAIResponse && internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {
		if prevID := internalRequest.OpenAIPreviousResponseID(); prevID != "" {
			responsesReplayState = resolveResponsesReplayState(apiKeyID, group.ID, requestModel, internalRequest)
			if responsesReplayState != nil {
				log.Debugf("loaded HTTP replay state (apikey=%d, group=%d, model=%s, previous_response_id=%s, channel=%d, key=%d)",
					apiKeyID, group.ID, requestModel, prevID, responsesReplayState.ChannelID, responsesReplayState.ChannelKeyID)
				// 转换请求为自包含形式（移除 previous_response_id，合并历史）
				// BuildReplayRequest 返回 nil 表示合并失败，应保留原始请求
				if replayed := responsesReplayState.BuildReplayRequest(internalRequest); replayed != nil {
					internalRequest = replayed
					log.Debugf("HTTP replay request transformed (apikey=%d, removed previous_response_id, merged history)", apiKeyID)
				} else {
					log.Warnf("HTTP replay history merge failed (apikey=%d, group=%d, model=%s, previous_response_id=%s), keeping original request",
						apiKeyID, group.ID, requestModel, prevID)
					responsesReplayState = nil // 放弃 replay，使用原始请求
				}
			} else {
				log.Debugf("no HTTP replay state found (apikey=%d, group=%d, model=%s, previous_response_id=%s)",
					apiKeyID, group.ID, requestModel, prevID)
			}
		}
	}

	// 创建迭代器（转换质量排序 + 策略排序 + 粘性优先）
	// 如果有 replay state，注入为 sticky 偏好
	var preferredSticky *balancer.SessionEntry
	if responsesReplayState != nil {
		preferredSticky = responsesReplayStateToSticky(responsesReplayState)
		if preferredSticky != nil {
			log.Debugf("HTTP replay sticky routing preference (channel=%d, key=%d)", preferredSticky.ChannelID, preferredSticky.ChannelKeyID)
		}
	}
	capabilityPolicy := getCapabilityDegradationPolicy()
	capabilityPlanner := newRelayCapabilityPlanner(internalRequest, rawBody, false)
	iter := balancer.NewIteratorWithPreferenceAndQuality(group, apiKeyID, requestModel, preferredSticky, func(item dbmodel.GroupItem) int {
		channel, _ := candidateSnapshot.Channel(item.ChannelID)
		return capabilityPlanner.rankChannel(channel, item)
	})
	if iter.Len() == 0 {
		writeInboundProtocolError(c, nil, inAdapter, relayProtocolError(http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel"))
		return
	}

	// === 早期心跳 ===
	// 在所有 forward / 重试 / 退避之前启动早期心跳协程，覆盖前置阶段（连接慢、failover、退避叠加）
	// 期间向客户端发 SSE 注释字节，避免被 Cloudflare 在 120s 零字节阈值上判 524。
	// 仅对流式请求生效；非流式无法发送 SSE 注释（破坏 application/json 协议），
	// 非流式请求由下方的请求级故障转移截止时间约束完整上游响应。
	isStream := internalRequest.Stream != nil && *internalRequest.Stream
	hb := startEarlyHeartbeat(c, isStream)
	defer hb.Stop()

	// 初始化 Metrics
	metrics := NewRelayMetrics(apiKeyID, requestModel, relayEndpointType(inboundType), middleware.ClientIP(c), rawBody, internalRequest)
	// 如果触发了 HTTP replay，记录 ws_mode=replay 和 ws_recovery=replay
	if responsesReplayState != nil {
		metrics.SetWSMode(dbmodel.RelayLogWSModeReplay)
		metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReplay)
	}
	// 请求级上下文
	req := &relayRequest{
		c:                 c,
		inAdapter:         inAdapter,
		inboundType:       inboundType,
		internalRequest:   internalRequest,
		metrics:           metrics,
		apiKeyID:          apiKeyID,
		requestModel:      requestModel,
		groupID:           group.ID,
		groupSessionTTL:   group.SessionKeepTime,
		iter:              iter,
		capabilityPolicy:  capabilityPolicy,
		capabilityPlanner: capabilityPlanner,
		candidateSnapshot: candidateSnapshot,
		rawBody:           rawBody,
		heartbeat:         hb,
	}

	var lastErr error
	var lastResult attemptResult
	var lastAttempt *relayRequest
	var capabilityErr error
	var capabilityResult attemptResult
	var sawSupportedCapability bool

	// max_retries 为兼容字段，运行时语义是“包含首次请求的最大尝试次数”。
	maxSameChannelAttempts := sameChannelMaxAttempts(group.RetryEnabled, group.MaxRetries)
	failoverBudget := newRelayFailoverBudget(time.Now())
	rateLimitedChannels := make(map[int]struct{})

relayLoop:
	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Debugf("request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}
		if budgetErr := failoverBudget.timeError(time.Now()); budgetErr != nil {
			lastErr = budgetErr
			lastResult = relayBudgetAttemptResult(budgetErr)
			log.Warnf("relay failover budget exhausted: %v", budgetErr)
			break
		}

		item := iter.Item()
		if _, rateLimited := rateLimitedChannels[item.ChannelID]; rateLimited {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), "channel rate limited earlier in this request")
			continue
		}

		// 获取通道
		channel, err := candidateSnapshot.Channel(item.ChannelID)
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		// Validate the outbound type before selecting a key. Every actual retry
		// below receives a fresh stateful outbound adapter.
		candidateAdapter := outbound.Get(channel.Type)
		if candidateAdapter == nil {
			iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		decision := planRelayCapability(req, channel, candidateAdapter, item.ModelName)
		logRelayCapability(channel, item.ModelName, decision, capabilityPolicy)
		if reject, errorCode := evaluateCapabilityPolicy(decision, capabilityPolicy); reject {
			message := capabilityRejectionMessage(decision, channel.Type.String())
			iter.SkipWithCapability(channel.ID, 0, channel.Name, message, capabilityTrace(decision, capabilityPolicy, channel.Type.String()))
			candidateErr := fmt.Errorf("capability rejected: %s", message)
			candidateResult := attemptResult{
				Err:           candidateErr,
				StatusCode:    http.StatusBadRequest,
				ProtocolError: relayProtocolError(http.StatusBadRequest, errorCode, message),
			}
			capabilityErr, capabilityResult = preferCapabilityRejection(capabilityErr, capabilityResult, candidateErr, candidateResult)
			continue
		}
		sawSupportedCapability = true

		log.Debugf("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
			requestModel, group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky())

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:   make(map[int]struct{}),
			PreferredKeyID:  iter.StickyKeyID(),
			InFlightPenalty: 1,
		}
		var usedKey dbmodel.ChannelKey
		releaseKey := func() {}
		for {
			usedKey, releaseKey = balancer.SelectAndReserveChannelKey(channel, selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if !iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				break
			}
			releaseKey()
			selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
			usedKey = dbmodel.ChannelKey{}
		}
		if usedKey.ChannelKey == "" {
			if len(selectOpts.ExcludeKeyIDs) == 0 {
				iter.Skip(channel.ID, 0, channel.Name, "no available key")
			} else {
				iter.InvalidateCurrentPreference()
			}
			continue
		}
		if budgetErr := failoverBudget.reserveChannel(channel.ID, time.Now()); budgetErr != nil {
			releaseKey()
			lastErr = budgetErr
			lastResult = relayBudgetAttemptResult(budgetErr)
			log.Warnf("relay failover budget exhausted: %v", budgetErr)
			break
		}

		// 同通道重试循环
		var result attemptResult
		for attemptNum := 0; attemptNum < maxSameChannelAttempts; attemptNum++ {
			// 重试前等待退避
			if attemptNum > 0 {
				// 预检查避免在已无尝试配额时仍等待 Retry-After。
				if budgetErr := failoverBudget.attemptError(time.Now()); budgetErr != nil {
					lastErr = budgetErr
					lastResult = relayBudgetAttemptResult(budgetErr)
					log.Warnf("relay failover budget exhausted: %v", budgetErr)
					releaseKey()
					break relayLoop
				}
				delay := computeAttemptBackoff(attemptNum, result.RetryAt, result.RetryAfter)
				log.Infof("same-channel retry %d/%d for %s, waiting %v",
					attemptNum, maxSameChannelAttempts-1, channel.Name, delay)
				if waitErr := failoverBudget.wait(c.Request.Context(), delay); waitErr != nil {
					if isLocalRelayBudgetExceeded(nil, waitErr) {
						lastErr = waitErr
						lastResult = relayBudgetAttemptResult(waitErr)
						log.Warnf("relay failover budget exhausted: %v", waitErr)
						releaseKey()
						break relayLoop
					}
					log.Debugf("request context canceled during retry backoff")
					releaseKey()
					metrics.SaveWithChannelStats(c.Request.Context(), false, waitErr, iter.Attempts(), false)
					return
				}
			}
			attemptCtx := c.Request.Context()
			cancelAttempt := func() {}
			if !isStream {
				attemptCtx, cancelAttempt = failoverBudget.attemptContext(attemptCtx)
			}
			attemptRequest, attemptErr := newAttemptRelayRequest(req, attemptCtx, item.ModelName)
			if attemptErr != nil {
				cancelAttempt()
				classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to prepare relay attempt: %w", attemptErr))
				result = attemptResult{
					Err:           classified,
					StatusCode:    http.StatusInternalServerError,
					Failure:       FailureClassification{Class: FailureConfiguration, StatusCode: http.StatusInternalServerError},
					ProtocolError: relayProtocolError(http.StatusInternalServerError, CodeRelayConfiguration, classified.Error()),
				}
				break
			}

			// 构造尝试级上下文
			ra := &relayAttempt{
				relayRequest:           attemptRequest,
				outAdapter:             outbound.Get(channel.Type),
				channel:                channel,
				usedKey:                usedKey,
				firstTokenTimeOutSec:   group.FirstTokenTimeOut,
				failoverDeadline:       failoverBudget.precommitDeadline(),
				emptyResponseDetection: emptyResponseDetection,
				capabilityDecision:     decision,
			}
			if budgetErr := failoverBudget.reserveAttempt(time.Now()); budgetErr != nil {
				cancelAttempt()
				lastErr = budgetErr
				lastResult = relayBudgetAttemptResult(budgetErr)
				log.Warnf("relay failover budget exhausted: %v", budgetErr)
				releaseKey()
				break relayLoop
			}

			result = ra.attempt()
			cancelAttempt()
			lastAttempt = attemptRequest
			if result.EmptyResponse {
				log.Warnf("empty response from channel %s (model=%s, stream=%t, retry %d/%d)",
					channel.Name, item.ModelName,
					req.internalRequest.Stream != nil && *req.internalRequest.Stream,
					attemptNum+1, maxSameChannelAttempts)
			}
			if result.Failure.Class == FailureBudgetExceeded {
				lastErr = result.Err
				lastResult = relayBudgetAttemptResult(result.Err)
				log.Warnf("relay failover budget exhausted: %v", result.Err)
				releaseKey()
				break relayLoop
			}
			switchTime := time.Now()
			if !result.Written && !result.Canceled && !result.ResetConversation &&
				result.Failure.Class == FailureRateLimit &&
				iter.HasRemainingDifferentChannelMatching(channel.ID, rateLimitedChannels, func(candidateChannelID int) bool {
					return failoverBudget.canAttemptChannel(candidateChannelID, switchTime)
				}) {
				rateLimitedChannels[channel.ID] = struct{}{}
				log.Infof("channel %s rate limited; switching to a remaining channel without same-channel backoff", channel.Name)
				break
			}
			if result.Success || result.Written || result.Canceled || result.ResetConversation || result.FirstTokenTimeout || !result.Failure.Retryable {
				break
			}
		}
		releaseKey()

		// 同通道重试耗尽后记录熔断器失败
		if !result.Success && !result.Written && !result.Canceled && !result.ResetConversation && result.Failure.Record {
			failureKind := failureCircuitKind(result.Failure)
			result.RetryAt = recordFailureAndResolveRetryAt(channel.ID, usedKey.ID, item.ModelName, result.Failure, result.RetryAt)
			result.Failure.RetryAt = result.RetryAt
			if failureKind == balancer.FailureTransient {
				maybeLearnManagedRoute(c.Request.Context(), channel.ID, item.ModelName, inboundType, result.Err)
			}
		}

		if result.Success {
			// === HTTP Replay 状态保存 ===
			// 成功后，如果是 OpenAI Responses HTTP 请求，保存 replay 状态供后续续接
			// 注意：exact replay 请求成功后也需要保存新状态，否则只能续接一轮
			// 优先使用 metrics.InternalResponse（streaming 安全），避免二次 GetInternalResponse 消耗聚合器
			if inboundType == inbound.InboundTypeOpenAIResponse && lastAttempt != nil &&
				lastAttempt.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {
				internalResponse := metrics.InternalResponse
				if internalResponse == nil {
					var err error
					internalResponse, err = lastAttempt.inAdapter.GetInternalResponse(c.Request.Context())
					if err != nil {
						log.Debugf("failed to get internal response for replay state save: %v", err)
					}
				}
				if internalResponse != nil {
					// 如果是 exact replay 请求，基于已有状态继续累积
					var newState *wsConversationState
					if lastAttempt.internalRequest.IsOpenAIExactReplayRequest() && responsesReplayState != nil {
						newState = cloneWSConversationState(responsesReplayState)
						if newState != nil {
							newState.ChannelID = channel.ID
							newState.ChannelKeyID = usedKey.ID
						}
					}
					if newState == nil {
						newState = &wsConversationState{
							RequestModel: requestModel,
							ChannelID:    channel.ID,
							ChannelKeyID: usedKey.ID,
						}
					}
					newState.ApplySuccessfulTurn(lastAttempt.internalRequest, internalResponse)
					if newState.LastResponseID != "" {
						ttl := wsConversationStateTTL(group.SessionKeepTime)
						storeResponsesReplayState(apiKeyID, group.ID, requestModel, newState, ttl)
						log.Debugf("saved HTTP replay state (apikey=%d, group=%d, model=%s, response_id=%s, channel=%d, key=%d, ttl=%v, is_replay=%t)",
							apiKeyID, group.ID, requestModel, newState.LastResponseID, channel.ID, usedKey.ID, ttl, lastAttempt.internalRequest.IsOpenAIExactReplayRequest())
					}
				}
			}

			metrics.SaveWithChannelStats(c.Request.Context(), true, nil, iter.Attempts(), false)
			return
		}
		if result.Canceled {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			return
		}
		if result.ResetConversation {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			errorAdapter := req.inAdapter
			if lastAttempt != nil {
				errorAdapter = lastAttempt.inAdapter
			}
			if publicErr, ok := classifyWSPublicError(result.Err, result.StatusCode); ok {
				writeInboundProtocolError(c, hb, errorAdapter, relayProtocolError(publicErr.Status, CodeRelayUpstreamFailed, publicErr.Message))
			} else {
				writeInboundProtocolError(c, hb, errorAdapter, protocolErrorFromError(result.StatusCode, result.Err))
			}
			return
		}
		if result.Written {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			return
		}
		iter.InvalidateCurrentPreference()
		lastErr = result.Err
		lastResult = result
	}

	// 所有候选通道均失败。Capability 拒绝只有在不存在任何可接受候选时
	// 才成为最终错误，避免覆盖已经发生的真实上游失败。
	lastErr, lastResult = resolveFinalAttemptResult(
		sawSupportedCapability,
		lastErr,
		lastResult,
		capabilityErr,
		capabilityResult,
	)
	metrics.SaveWithChannelStats(c.Request.Context(), false, lastErr, iter.Attempts(), false)
	errorAdapter := req.inAdapter
	if lastAttempt != nil {
		errorAdapter = lastAttempt.inAdapter
	}

	// Rate limiting and temporary provider errors are returned with the exact
	// remaining Retry-After so downstream SDKs can make their own decision.
	if lastResult.Failure.Passthrough || isPassthroughStatus(lastResult.StatusCode) {
		if value := retryAfterHeaderValue(lastResult.RetryAt, time.Now()); value != "" {
			c.Header("Retry-After", value)
		} else if lastResult.RetryAfter > 0 {
			c.Header("Retry-After", retryAfterDurationHeaderValue(lastResult.RetryAfter))
		}
		writeInboundProtocolError(c, hb, errorAdapter, protocolErrorForAttempt(lastResult, lastErr))
		return
	}
	if lastResult.StatusCode > 0 {
		writeInboundProtocolError(c, hb, errorAdapter, protocolErrorForAttempt(lastResult, lastErr))
		return
	}
	writeInboundProtocolError(c, hb, errorAdapter, protocolErrorForAttempt(lastResult, lastErr))
}

// newAttemptInboundAdapter builds the fresh inbound adapter owned by one
// upstream attempt. Response-side state must stay isolated per attempt, but
// request-derived state is seeded from the canonical parsed request instead
// of re-running TransformRequest (JSON parse, protocol conversion, and
// token counting) on the unchanged body for every retry.
func newAttemptInboundAdapter(inboundType inbound.InboundType, seed *model.InternalLLMRequest) (model.Inbound, error) {
	adapter := inbound.Get(inboundType)
	if adapter == nil {
		return nil, fmt.Errorf("unsupported inbound type: %d", inboundType)
	}
	if seedable, ok := adapter.(model.RequestStateSeedable); ok {
		seedable.SeedRequestState(seed)
	}
	return adapter, nil
}

func planRelayCapability(req *relayRequest, channel *dbmodel.Channel, adapter model.Outbound, modelName string) outbound.CapabilityDecision {
	if req == nil || req.internalRequest == nil {
		return outbound.PlanRequestForModel(nil, "", outbound.OutboundType(0), false)
	}
	if req.capabilityPlanner == nil {
		// Direct/unit callers may construct relayRequest themselves. Lazily
		// attach the same request-scoped cache used by the main handlers.
		req.capabilityPlanner = newRelayCapabilityPlanner(req.internalRequest, req.rawBody, req.c == nil)
	}
	return req.capabilityPlanner.plan(channel, adapter, modelName)
}

func planRelayPassthrough(request *model.InternalLLMRequest, rawBody []byte, channel *dbmodel.Channel, adapter model.Outbound, websocketIngress bool, overrideConfigured ...bool) bool {
	if request == nil || channel == nil || adapter == nil || len(rawBody) == 0 {
		return false
	}
	if channelParamOverrideActive(channel) || (len(overrideConfigured) > 0 && overrideConfigured[0]) {
		return false
	}
	capable, ok := adapter.(model.PassthroughCapable)
	if !ok || !capable.CanPassthrough(request.RawAPIFormat) {
		return false
	}

	passthrough := channel.AllowsPassthrough()
	if request.HasOpenAIResponsesPassthrough() && !websocketIngress {
		passthrough = true
	}
	if request.RawAPIFormat != model.APIFormatOpenAIResponse {
		return passthrough
	}
	if request.IsOpenAIExactReplayRequest() {
		return false
	}
	if websocketIngress {
		return channel.Type == outbound.OutboundTypeOpenAIResponse &&
			shouldEnableResponsesWS(channel) && effectiveResponsesWSMode(channel) == responsesWSModePassthrough
	}
	if requiresUpstreamWSContinuation(request) {
		return false
	}
	return passthrough
}

type capabilityDegradationPolicy string

const (
	capabilityPolicyAllow  capabilityDegradationPolicy = "allow"
	capabilityPolicyWarn   capabilityDegradationPolicy = "warn"
	capabilityPolicyStrict capabilityDegradationPolicy = "strict"
)

func getCapabilityDegradationPolicy() capabilityDegradationPolicy {
	value, err := op.SettingGetString(dbmodel.SettingKeyCapabilityDegradationPolicy)
	if err != nil {
		return capabilityPolicyWarn
	}
	policy := capabilityDegradationPolicy(strings.ToLower(strings.TrimSpace(value)))
	switch policy {
	case capabilityPolicyAllow, capabilityPolicyWarn, capabilityPolicyStrict:
		return policy
	default:
		return capabilityPolicyWarn
	}
}

func capabilityRank(decision outbound.CapabilityDecision) int {
	if decision.Rejected() {
		return 3
	}
	if decision.Status == outbound.CapabilityDegraded {
		return 2
	}
	if decision.StaticQuality == outbound.QualityNative {
		return 0
	}
	return 1
}

func logRelayCapability(channel *dbmodel.Channel, modelName string, decision outbound.CapabilityDecision, policy capabilityDegradationPolicy) {
	outbound.RecordCapabilityDecision(decision)
	if channel == nil {
		return
	}
	log.Debugw("relay.capability_decision",
		"channel_id", channel.ID,
		"channel", channel.Name,
		"model", modelName,
		"status", decision.Status,
		"conversion_path", decision.ConversionPath,
		"required_features", decision.RequiredFeatures,
		"degraded_fields", decision.DegradedFields,
		"losses", decision.Losses,
		"lossiness", decision.Lossiness,
		"static_quality", decision.StaticQuality,
		"reasons", decision.Reasons,
	)
	if decision.Status == outbound.CapabilityDegraded {
		if policy == capabilityPolicyWarn {
			log.Warnw("relay.capability_degraded", "channel_id", channel.ID, "channel", channel.Name, "model", modelName, "fields", decision.DegradedFields, "losses", decision.Losses, "reasons", decision.Reasons)
		} else if policy == capabilityPolicyAllow {
			log.Debugw("relay.capability_degraded_allowed", "channel_id", channel.ID, "channel", channel.Name, "model", modelName, "fields", decision.DegradedFields)
		}
	}
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	defer ra.closeFirstTokenBudget()

	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name)
	span.SetAdapterType(ra.channel.Type.String())
	span.SetCapability(capabilityTrace(ra.capabilityDecision, ra.capabilityPolicy, ra.channel.Type.String()))

	// 转发请求
	statusCode, fwdErr := ra.forward()
	// Some transports surface a canceled read instead of the cancel cause. Do
	// the cause translation once at the attempt boundary so HTTP error bodies,
	// transformed streams, and WS passthrough share the same timeout semantics.
	if fwdErr != nil &&
		!isLocalRelayBudgetExceeded(nil, fwdErr) &&
		!isFirstTokenTimeout(nil, fwdErr) {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ra.requestContext(), fwdErr); timeoutErr != nil {
			fwdErr = timeoutErr
		}
	}

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		// ====== 成功 ======
		// Passthrough handlers collect response at stream end via PassthroughConfig.CollectMetrics
		ra.collectResponse()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		op.ChannelKeyUpdate(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.breakerModelName())
		// Refresh model affinity only after the complete response succeeds.
		balancer.SetRoutingAffinity(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	if isClientCancellation(ra.requestContext(), fwdErr) {
		written := ra.streamPayloadWritten.Load()
		if written {
			ra.collectResponse()
		}
		op.ChannelKeyUpdate(ra.usedKey)
		span.SetFailure(string(FailureClientCanceled), false, time.Time{})
		span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())
		return attemptResult{
			Success:    false,
			Written:    written,
			Canceled:   true,
			Err:        fwdErr,
			StatusCode: statusCode,
			Failure:    FailureClassification{Class: FailureClientCanceled, StatusCode: statusCode},
		}
	}

	failure := classifyRelayFailureContext(ra.requestContext(), statusCode, fwdErr, ra.retryAt)
	span.SetFailure(string(failure.Class), failure.Retryable, failure.RetryAt)
	op.ChannelKeyUpdate(ra.usedKey)
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())

	// Channel 维度统计
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})

	// 注意：熔断器记录已移至 Handler() 的同通道重试循环外，
	// 避免重试期间过早触发熔断

	written := ra.streamPayloadWritten.Load()
	if written {
		ra.collectResponse()
		if responseError := protocolErrorFromAttempt(ra.upstreamError, statusCode, fwdErr); responseError != nil {
			writeStreamProtocolError(ra.requestContext(), ra.getStreamWriter(), ra.inAdapter, responseError)
		}
	}
	firstTokenTimeout := isFirstTokenTimeout(nil, fwdErr)
	return attemptResult{
		Success:           false,
		Written:           written,
		ResetConversation: statusCode == http.StatusConflict && needsConversationRestart(relayErrorMessage(fwdErr)),
		FirstTokenTimeout: firstTokenTimeout,
		EmptyResponse:     errors.Is(fwdErr, stream.ErrEmptyUpstreamStream),
		Err:               fmt.Errorf("channel %s failed: %w", ra.channel.Name, fwdErr),
		StatusCode:        statusCode,
		RetryAfter:        ra.retryAfter,
		RetryAt:           ra.retryAt,
		Failure:           failure,
		ProtocolError:     protocolErrorFromAttempt(ra.upstreamError, statusCode, fwdErr),
	}
}

func (ra *relayAttempt) breakerModelName() string {
	if ra != nil && ra.iter != nil && ra.iter.Index() >= 0 && ra.iter.Index() < ra.iter.Len() {
		if modelName := strings.TrimSpace(ra.iter.Item().ModelName); modelName != "" {
			return modelName
		}
	}
	if ra != nil && ra.internalRequest != nil {
		return strings.TrimSpace(ra.internalRequest.Model)
	}
	return ""
}

func protocolErrorFromAttempt(upstreamError *model.ResponseError, statusCode int, err error) *model.ResponseError {
	if upstreamError != nil {
		return model.NormalizeResponseError(upstreamError, statusCode, "api_error")
	}
	var responseError *model.ResponseError
	if errors.As(err, &responseError) {
		return model.NormalizeResponseError(responseError, statusCode, "api_error")
	}
	if statusCode <= 0 {
		return nil
	}
	return protocolErrorFromError(statusCode, err)
}

// parseRequest 解析并验证入站请求
// 返回值中的 rawBody 为客户端原始请求字节，供同格式直通路径重用。
func parseRequest(inboundType inbound.InboundType, c *gin.Context) ([]byte, *model.InternalLLMRequest, model.Inbound, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		// middleware.MaxBodySize 命中时要回 413，别把限流当成内部错误。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			resp.ErrorWithCode(c, http.StatusRequestEntityTooLarge, CodeRelayRequestTooLarge, "request body too large")
			return nil, nil, nil, err
		}
		resp.ErrorWithCode(c, http.StatusInternalServerError, CodeRelayUpstreamFailed, err.Error())
		return nil, nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	transformCtx := c.Request.Context()
	signatureScope := compat.GeminiSignatureScopeFromContext(transformCtx)
	if apiKeyID := c.GetInt("api_key_id"); apiKeyID > 0 {
		signatureScope.APIKeyID = strconv.Itoa(apiKeyID)
		transformCtx = compat.WithGeminiSignatureScope(transformCtx, signatureScope)
		c.Request = c.Request.WithContext(transformCtx)
	}
	internalRequest, err := inAdapter.TransformRequest(transformCtx, body)
	if err != nil {
		writeInboundProtocolError(c, nil, inAdapter, relayProtocolError(http.StatusBadRequest, "invalid_request", err.Error()))
		return nil, nil, nil, err
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		writeInboundProtocolError(c, nil, inAdapter, relayProtocolError(http.StatusBadRequest, "invalid_request", err.Error()))
		return nil, nil, nil, err
	}

	return body, internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.requestContext()
	ctx = ra.startFirstTokenBudget(ctx)

	// 尝试上游 WebSocket（仅 OpenAI Response outbound 类型；必须是客户端 WS 入站且新开关显式启用）
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse &&
		ra.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {

		shouldTryWS := false
		// Passthrough is now handled by forwardViaHTTP via PassthroughCapable interface
		if ra.internalRequest.IsOpenAIExactReplayRequest() {
			shouldTryWS = false
		} else if ra.c == nil {
			wsMode := effectiveResponsesWSMode(ra.channel)
			shouldTryWS = shouldEnableResponsesWS(ra.channel) && wsMode != responsesWSModeOff
		} else if requiresUpstreamWSContinuation(ra.internalRequest) {
			// Safety: HTTP ingress must not proactively use upstream WS for fresh requests,
			// but an explicit continuation cannot be safely failovered as ordinary HTTP.
			shouldTryWS = true
		}

		if shouldTryWS {
			statusCode, err := ra.forwardViaWS(ctx)
			if statusCode != -1 {
				return statusCode, err
			}
			if requiresUpstreamWSContinuation(ra.internalRequest) {
				balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.requestModel)
				return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
			}
			if ra.internalRequest.HasOpenAIResponsesPassthrough() {
				return http.StatusBadGateway, fmt.Errorf("upstream WS passthrough unavailable for native-only Responses semantics")
			}
			ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryDowngrade)
			// statusCode == -1 means WS not available, fall through to HTTP
		}
	}

	return ra.forwardViaHTTP(ctx)
}

// forwardViaWS attempts to forward via upstream WebSocket.
// Returns statusCode=-1 if WS is not available (caller should fall through to HTTP).
func (ra *relayAttempt) forwardViaWS(ctx context.Context) (int, error) {
	if ra.c == nil && effectiveResponsesWSMode(ra.channel) == responsesWSModePassthrough && !ra.internalRequest.IsOpenAIExactReplayRequest() && !channelParamOverrideActive(ra.channel) {
		return ra.forwardViaWSPassthrough(ctx)
	}
	continuation := requiresUpstreamWSContinuation(ra.internalRequest)
	preferredConnID := ""
	if continuation {
		preferredConnID, _ = getWSResponseConn(currentPreviousResponseID(ra.internalRequest))
	}
	pc := TryUpstreamWSWithPreference(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), preferredConnID)
	if pc == nil {
		log.Debugf("upstream WS unavailable for channel %s (key=%d, continuation=%t)", ra.channel.Name, ra.usedKey.ID, continuation)
		return -1, nil // WS not available
	}

	log.Debugf("using upstream WebSocket for channel %s (key=%d)", ra.channel.Name, ra.usedKey.ID)
	log.Debugf("upstream WS selected (channel=%s, key=%d, continuation=%t, previous_response_id=%s)",
		ra.channel.Name, ra.usedKey.ID, continuation, currentPreviousResponseID(ra.internalRequest))

	// Build the Responses API request body
	responsesReq := openaiOutbound.ConvertToResponsesRequest(ra.internalRequest)
	reqBody, err := json.Marshal(responsesReq)
	if err != nil {
		wsUpstreamPool.Put(pc)
		return 0, classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to build websocket request: %w", err))
	}
	reqBody, err = buildWSResponseCreateMessage(reqBody)
	if err != nil {
		wsUpstreamPool.Put(pc)
		return 0, classifyLocalRelayError(FailureConfiguration, err)
	}
	reqBody, err = ra.applyParamOverridePayload(reqBody)
	if err != nil {
		wsUpstreamPool.Put(pc)
		return 0, err
	}
	if err := validateWSResponseCreatePayload(reqBody); err != nil {
		wsUpstreamPool.Put(pc)
		return 0, classifyLocalRelayError(FailureConfiguration, err)
	}

	// Send response.create message
	if err := wsUpstreamPool.SendRaw(ctx, pc, reqBody); err != nil {
		log.Warnf("upstream WS send failed for channel %s: %v", ra.channel.Name, err)
		log.Debugf("upstream WS send failed before stream start (channel=%s, key=%d, continuation=%t, err=%v)",
			ra.channel.Name, ra.usedKey.ID, continuation, err)
		wsUpstreamPool.RemoveConn(pc)
		if isUpstreamWSConnectionBroken(err) {
			log.Debugf("upstream WS send failure eligible for redial (channel=%s, key=%d, continuation=%t)",
				ra.channel.Name, ra.usedKey.ID, continuation)
			statusCode, redialErr, recovered := ra.retryViaFreshUpstreamWS(ctx, reqBody)
			if recovered || redialErr != nil {
				return statusCode, redialErr
			}
			if requiresUpstreamWSContinuation(ra.internalRequest) {
				balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.requestModel)
				return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
			}
		}
		wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		return -1, nil // fall through to HTTP
	}

	// Read events from WS and process through the transform pipeline
	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModeTransform)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	reader := newWSUpstreamReader(pc, ra.channel.ID, ra.usedKey.ID)
	err = ra.handleWSStreamResponseV2(ctx, reader)
	if err != nil {
		ra.captureRetryAt(reader.RetryAt())
		reader.CloseWithError()
		log.Debugf("upstream WS stream failed (channel=%s, key=%d, continuation=%t, written=%t, status=%d, err=%v)",
			ra.channel.Name, ra.usedKey.ID, continuation, ra.getStreamWriter().Written(), reader.StatusCode(), err)
		if requiresUpstreamWSContinuation(ra.internalRequest) && !ra.streamPayloadWritten.Load() && shouldReconnectUpstreamWSBeforeReplay(err) {
			log.Debugf("upstream WS stream failure eligible for reconnect before replay (channel=%s, key=%d, previous_response_id=%s)",
				ra.channel.Name, ra.usedKey.ID, currentPreviousResponseID(ra.internalRequest))
			statusCode, redialErr, recovered := ra.retryViaFreshUpstreamWS(ctx, reqBody)
			if recovered || redialErr != nil {
				return statusCode, redialErr
			}
		}
		if requiresUpstreamWSContinuation(ra.internalRequest) && isContinuationTransportFailure(err) {
			balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
		}
		if ra.requestContext().Err() == nil {
			wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		}
		return reader.StatusCode(), err
	}

	reader.Close()
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.recordSuccessfulWSAffinity(pc)
	return 200, nil
}

func (ra *relayAttempt) retryViaFreshUpstreamWS(ctx context.Context, reqBody []byte) (int, error, bool) {
	log.Debugf("attempting fresh upstream WS redial (channel=%s, key=%d, previous_response_id=%s)",
		ra.channel.Name, ra.usedKey.ID, currentPreviousResponseID(ra.internalRequest))
	redialed := TryUpstreamWS(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), true)
	if redialed == nil {
		log.Debugf("fresh upstream WS redial unavailable (channel=%s, key=%d)", ra.channel.Name, ra.usedKey.ID)
		return 0, nil, false
	}

	retryErr := wsUpstreamPool.SendRaw(ctx, redialed, reqBody)
	if retryErr != nil {
		log.Warnf("upstream WS redial send failed for channel %s: %v", ra.channel.Name, retryErr)
		log.Debugf("fresh upstream WS redial send failed (channel=%s, key=%d, err=%v)", ra.channel.Name, ra.usedKey.ID, retryErr)
		wsUpstreamPool.RemoveConn(redialed)
		wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		if requiresUpstreamWSContinuation(ra.internalRequest) {
			balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation"), true
		}
		return -1, nil, true
	}

	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModeTransform)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReconnect)
	reader := newWSUpstreamReader(redialed, ra.channel.ID, ra.usedKey.ID)
	streamErr := ra.handleWSStreamResponseV2(ctx, reader)
	if streamErr != nil {
		ra.captureRetryAt(reader.RetryAt())
		reader.CloseWithError()
		log.Debugf("fresh upstream WS redial stream failed (channel=%s, key=%d, status=%d, err=%v)",
			ra.channel.Name, ra.usedKey.ID, reader.StatusCode(), streamErr)
		if requiresUpstreamWSContinuation(ra.internalRequest) && isContinuationTransportFailure(streamErr) {
			balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation"), true
		}
		if ra.requestContext().Err() == nil {
			wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		}
		return reader.StatusCode(), streamErr, true
	}
	log.Debugf("fresh upstream WS redial succeeded (channel=%s, key=%d, previous_response_id=%s)",
		ra.channel.Name, ra.usedKey.ID, currentPreviousResponseID(ra.internalRequest))
	reader.Close()
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.recordSuccessfulWSAffinity(redialed)
	return http.StatusOK, nil, true
}

func isContinuationTransportFailure(err error) bool {
	// Check for empty stream error (both old message and new error type)
	if errors.Is(err, stream.ErrEmptyUpstreamStream) {
		return true
	}
	message := relayErrorMessage(err)
	return isUpstreamWSConnectionBroken(err) ||
		needsConversationRestart(message) ||
		strings.Contains(message, "ws stream ended before first event")
}

func (ra *relayAttempt) clientRequestHeaders() http.Header {
	if ra == nil || ra.c == nil || ra.c.Request == nil {
		return nil
	}
	return ra.c.Request.Header
}

func (ra *relayAttempt) handleWSStreamResponseV2(ctx context.Context, reader *wsUpstreamReader) error {
	defer ra.closeFirstTokenBudget()

	// Hand off early heartbeat
	ra.heartbeat.Hand()

	// Build transform function
	semanticPayload := false
	transform := func(ctx context.Context, data []byte) ([]byte, error) {
		var err error
		var output []byte
		output, semanticPayload, err = ra.transformStreamData(ctx, string(data))
		return output, err
	}
	precommit := func(_, _ []byte) bool { return semanticPayload }

	// Determine first token timeout
	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:             stream.NewWSSource(reader),
		Transform:          transform,
		Writer:             ra.getStreamWriter(),
		Context:            ctx,
		FirstTokenTimeout:  firstTokenTimeout,
		HeartbeatInterval:  streamHeartbeatInterval(),
		PrecommitPredicate: precommit,
		PrecommitMaxEvents: 8,
		PrecommitMaxBytes:  64 * 1024,
		AllowEmptyPayload:  ra.allowEmptyPayload(),
		OnFirstToken: func() {
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.stopFirstTokenTimer()
		},
		OnFinish: func(context.Context) error {
			return ra.finalizeStreamLifecycle(ctx, true)
		},
	})

	// Run processor
	err := processor.Run()

	// Track payload written for metrics collection
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}

	// Handle first token timeout specifically
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		return ra.firstTokenTimeoutError()
	}

	// Check for context cancellation with first token timeout
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}

	return err
}

// forwardViaHTTP forwards the request using traditional HTTP.
func (ra *relayAttempt) forwardViaHTTP(ctx context.Context) (int, error) {
	// Check for passthrough capability using interface
	if pt, ok := ra.outAdapter.(model.PassthroughCapable); ok && ra.shouldUseHTTPPassthrough(pt) {
		// Additional checks for OpenAI Responses edge cases
		if ra.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {
			if ra.c == nil || ra.internalRequest.IsOpenAIExactReplayRequest() || requiresUpstreamWSContinuation(ra.internalRequest) {
				// Fall through to standard path
			} else {
				return ra.forwardViaHTTPPassthrough(ctx, pt)
			}
		} else {
			return ra.forwardViaHTTPPassthrough(ctx, pt)
		}
	}

	return ra.forwardViaHTTPStandard(ctx)
}

func (ra *relayAttempt) shouldUseHTTPPassthrough(pt model.PassthroughCapable) bool {
	if ra == nil || pt == nil || ra.channel == nil || ra.internalRequest == nil || len(ra.rawBody) == 0 {
		return false
	}
	if channelParamOverrideActive(ra.channel) {
		return false
	}
	if !pt.CanPassthrough(ra.internalRequest.RawAPIFormat) {
		return false
	}
	return ra.channel.AllowsPassthrough() || ra.internalRequest.HasOpenAIResponsesPassthrough()
}

// forwardViaHTTPPassthrough handles unified passthrough for any PassthroughCapable transformer.
func (ra *relayAttempt) forwardViaHTTPPassthrough(ctx context.Context, pt model.PassthroughCapable) (int, error) {
	// Build request via TransformRequestRaw
	outboundRequest, err := pt.TransformRequestRaw(
		ctx,
		ra.rawBody,
		ra.internalRequest.Model,
		ra.channel.GetBaseUrl(),
		ra.usedKey.ChannelKey,
		ra.internalRequest.Query,
	)
	if err != nil {
		log.Warnf("failed to create passthrough request: %v", err)
		return 0, classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to create request: %w", err))
	}

	// Apply param overrides
	if err := ra.applyParamOverride(outboundRequest); err != nil {
		return 0, err
	}

	// Copy headers
	ra.copyHeaders(outboundRequest)
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse {
		outboundRequest.Header.Set("Content-Type", "application/json")
	}

	// Send request
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// Check status
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		ra.captureRetryAfter(response.Header.Get("Retry-After"))
		body, _ := readUpstreamErrorBody(response.Body)
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		ra.upstreamError = ra.outAdapter.TransformError(ctx, statusCode, response.Header, body)
		if ra.upstreamError == nil {
			ra.upstreamError = model.NormalizeHTTPError(statusCode, response.Header, body, "api_error")
		}
		log.Warnf("upstream error from channel %s: status=%d, body=%s", ra.channel.Name, response.StatusCode, string(body))
		return statusCode, ra.upstreamError
	}

	// Get passthrough config
	cfg := pt.PassthroughConfig()

	// Branch: streaming vs non-streaming
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		if err := ra.handleStreamResponsePassthroughV2(ctx, response, cfg); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if err := ra.handleResponsePassthrough(ctx, response, cfg); err != nil {
		return responseProcessingErrorStatus(response.StatusCode), err
	}
	return response.StatusCode, nil
}

// A successful HTTP status is not a successful relay attempt when response
// processing fails. Status 0 feeds the error into the existing retry/failover
// path; non-2xx statuses retain their original retry semantics.
func responseProcessingErrorStatus(status int) int {
	if status >= 200 && status < 300 {
		return 0
	}
	return status
}

// handleResponsePassthrough handles non-streaming passthrough responses.
func (ra *relayAttempt) handleResponsePassthrough(ctx context.Context, response *http.Response, cfg model.PassthroughConfig) error {
	body, err := httpio.ReadResponseBody(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Semantic precommit: validate and normalize the complete response before
	// writing the byte-stable passthrough body downstream.
	sidecarResp := &http.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, sidecarResp)
	if err != nil {
		return fmt.Errorf("failed to observe passthrough response: %w", err)
	}
	if internalResponse == nil {
		return fmt.Errorf("failed to observe passthrough response: empty canonical response")
	}
	if _, err := ra.inAdapter.TransformResponse(ctx, internalResponse); err != nil {
		return fmt.Errorf("failed to validate passthrough response: %w", err)
	}
	if ctx != nil && ctx.Err() != nil {
		return contextError(ctx)
	}
	ra.collectResponse()

	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	ra.c.Data(http.StatusOK, contentType, body)

	return nil
}

// forwardViaHTTPStandard 是 forwardViaHTTP 的原路径（直通判定失败时的兜底）。
// 留作显式出口，避免 passthrough 失败时的递归。
func (ra *relayAttempt) forwardViaHTTPStandard(ctx context.Context) (int, error) {
	outboundRequest, err := ra.outAdapter.TransformRequest(
		ctx,
		ra.internalRequest,
		ra.channel.GetBaseUrl(),
		ra.usedKey.ChannelKey,
	)
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to create request: %w", err))
	}
	if err := ra.applyParamOverride(outboundRequest); err != nil {
		return 0, err
	}

	// 复制请求头
	ra.copyHeaders(outboundRequest)
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse {
		outboundRequest.Header.Set("Content-Type", "application/json")
	}

	// 发送请求
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// 检查响应状态
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		ra.captureRetryAfter(response.Header.Get("Retry-After"))
		body, err := readUpstreamErrorBody(response.Body)
		if err != nil {
			return response.StatusCode, fmt.Errorf("failed to read response body: %w", err)
		}
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		ra.upstreamError = ra.outAdapter.TransformError(ctx, statusCode, response.Header, body)
		if ra.upstreamError == nil {
			ra.upstreamError = model.NormalizeHTTPError(statusCode, response.Header, body, "api_error")
		}
		log.Warnf("upstream error from channel %s: status=%d, body=%s", ra.channel.Name, response.StatusCode, string(body))
		return statusCode, ra.upstreamError
	}

	// 处理响应
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		// Use V2 StreamProcessor-based implementation
		if err := ra.handleStreamResponseV2(ctx, response); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if err := ra.handleResponse(ctx, response); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

func defaultWSModeForRequest(req *model.InternalLLMRequest) dbmodel.RelayLogWSMode {
	if requiresUpstreamWSContinuation(req) {
		return dbmodel.RelayLogWSModeContinuation
	}
	return dbmodel.RelayLogWSModeFresh
}

func readOutboundRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	// 出站 body 由入站 body 转换而来，本身已被 middleware.MaxBodySize 限住，
	// 这里用同一个上限兜底，避免 transformer 里的意外膨胀。
	if req.GetBody != nil {
		bodyReader, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer bodyReader.Close()
		return httpio.ReadResponseBody(bodyReader)
	}
	body, err := httpio.ReadResponseBody(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return body, nil
}

// getStreamWriter returns the appropriate stream writer for the current request.
func (ra *relayAttempt) getStreamWriter() StreamWriter {
	if ra.streamWriter != nil {
		return ra.streamWriter
	}
	return ra.c.Writer
}

// applyParamOverride merges channel-level JSON request overrides and records the final upstream payload.
func (ra *relayAttempt) applyParamOverride(outboundRequest *http.Request) error {
	originalBody, err := readOutboundRequestBody(outboundRequest)
	if err != nil {
		return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to inspect original outbound request body: %w", err))
	}
	requestBody, captured, err := helper.ApplyParamOverrideWithPayload(outboundRequest, ra.channel.ParamOverride)
	if err != nil {
		return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("invalid channel param override: %w", err))
	}
	if !captured {
		requestBody, err = readOutboundRequestBody(outboundRequest)
		if err != nil {
			return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to inspect outbound request body: %w", err))
		}
	}
	return ra.recordTransportRequestPayloadWithModelRequirement(requestBody, jsonPayloadHasTopLevelModel(originalBody))
}

// applyParamOverridePayload is the transport-neutral counterpart used by
// Responses WebSocket requests. It applies the exact same channel policy as
// the HTTP path and records the final bytes that will be sent upstream.
func (ra *relayAttempt) applyParamOverridePayload(payload []byte) ([]byte, error) {
	modified, _, err := helper.ApplyParamOverridePayload(payload, ra.channel.ParamOverride)
	if err != nil {
		return nil, classifyLocalRelayError(FailureConfiguration, fmt.Errorf("invalid channel param override: %w", err))
	}
	if err := ra.recordTransportRequestPayload(modified); err != nil {
		return nil, err
	}
	return modified, nil
}

func (ra *relayAttempt) recordTransportRequestPayload(payload []byte) error {
	return ra.recordTransportRequestPayloadWithModelRequirement(payload, false)
}

func (ra *relayAttempt) recordTransportRequestPayloadWithModelRequirement(payload []byte, requireModel bool) error {
	inspection := helper.InspectParamOverride(ra.channel.ParamOverride)
	if inspection.Valid {
		// A valid override may be configured on a transport with a non-JSON body
		// (multipart images, audio, or a provider-specific binary payload). The
		// helper deliberately leaves those bytes untouched; do not turn that
		// compatibility path into a configuration failure.
		if json.Valid(payload) {
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(payload, &envelope); err == nil && envelope != nil {
				if rawModel, ok := envelope["model"]; ok {
					var finalModel string
					if err := json.Unmarshal(rawModel, &finalModel); err != nil || strings.TrimSpace(finalModel) == "" {
						return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("channel param override produced an invalid model"))
					}
					if ra.internalRequest != nil {
						ra.internalRequest.Model = strings.TrimSpace(finalModel)
					}
				} else if requireModel || containsString(inspection.Paths, "/model") {
					return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("channel param override removed the required model"))
				}
			} else if requireModel {
				return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("channel param override produced a non-object request"))
			}
		} else if requireModel {
			return classifyLocalRelayError(FailureConfiguration, fmt.Errorf("channel param override produced invalid JSON"))
		}
	}
	if ra.metrics != nil {
		modelName := ""
		if ra.internalRequest != nil {
			modelName = ra.internalRequest.Model
		}
		ra.metrics.SetTransportRequestPayload(payload, modelName)
	}
	return nil
}

func (ra *relayAttempt) captureRetryAfter(header string) {
	ra.captureRetryAt(parseRetryAt(header))
}

func (ra *relayAttempt) captureRetryAt(retryAt time.Time) {
	ra.retryAt = retryAt
	ra.retryAfter = 0
	if !ra.retryAt.IsZero() {
		if delay := time.Until(ra.retryAt); delay > 0 {
			ra.retryAfter = delay
		}
	}
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request) {
	if ra.c != nil {
		for key, values := range ra.c.Request.Header {
			lowerKey := strings.ToLower(key)
			if hopByHopHeaders[lowerKey] {
				continue
			}
			// anthropic-beta 需要与出站默认值合并去重，避免覆盖掉
			// 透传路径预置的 prompt-caching / extended-cache-ttl 基线。
			if lowerKey == "anthropic-beta" {
				existing := outboundRequest.Header.Get(key)
				for _, value := range values {
					existing = mergeBetaHeader(existing, value)
				}
				if existing != "" {
					outboundRequest.Header.Set(key, existing)
				}
				continue
			}
			for _, value := range values {
				outboundRequest.Header.Set(key, value)
			}
		}
	}
	if outboundRequest.Header.Get("User-Agent") == "" {
		outboundRequest.Header.Set("User-Agent", "")
	}
	if len(ra.channel.CustomHeader) > 0 {
		for _, header := range ra.channel.CustomHeader {
			outboundRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

// mergeBetaHeader 合并两个逗号分隔的 anthropic-beta 字段值，去重并保留先后顺序。
func mergeBetaHeader(existing, incoming string) string {
	seen := make(map[string]struct{}, 8)
	merged := make([]string, 0, 8)
	for _, source := range []string{existing, incoming} {
		for _, entry := range strings.Split(source, ",") {
			normalized := strings.TrimSpace(entry)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			merged = append(merged, normalized)
		}
	}
	return strings.Join(merged, ",")
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, classifyLocalRelayError(FailureConfiguration, err)
	}

	req = ra.attachFirstTokenBudget(req)

	response, err := httpClient.Do(req)
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(req.Context(), err); timeoutErr != nil {
			ra.closeFirstTokenBudget()
			return nil, timeoutErr
		}
		if isClientCancellation(req.Context(), err) {
			log.Infof("request canceled before upstream response: %v", err)
		} else {
			log.Warnf("failed to send request: %v", err)
		}
		ra.closeFirstTokenBudget()
		return nil, err
	}

	if response != nil && response.Body != nil && ra.firstTokenBudget != nil {
		response.Body = &closeWithFuncReadCloser{
			ReadCloser: response.Body,
			onClose:    ra.closeFirstTokenBudget,
		}
	}

	return response, nil
}

// handleStreamResponseV2 uses StreamProcessor for unified stream handling.
func (ra *relayAttempt) handleStreamResponseV2(ctx context.Context, response *http.Response) error {
	defer ra.closeFirstTokenBudget()

	// Content-Type validation
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// Hand off early heartbeat
	ra.heartbeat.Hand()

	// Build transform function
	semanticPayload := false
	transform := func(ctx context.Context, data []byte) ([]byte, error) {
		var err error
		var output []byte
		output, semanticPayload, err = ra.transformStreamData(ctx, string(data))
		return output, err
	}
	precommit := func(_, _ []byte) bool { return semanticPayload }

	// Determine first token timeout
	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:             stream.NewSSESource(response.Body, maxSSEEventSize),
		Transform:          transform,
		Writer:             ra.getStreamWriter(),
		Context:            ctx,
		FirstTokenTimeout:  firstTokenTimeout,
		HeartbeatInterval:  streamHeartbeatInterval(),
		PrecommitPredicate: precommit,
		PrecommitMaxEvents: 8,
		PrecommitMaxBytes:  64 * 1024,
		AllowEmptyPayload:  ra.allowEmptyPayload(),
		OnFirstToken: func() {
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.stopFirstTokenTimer()
		},
		OnFinish: func(context.Context) error {
			return ra.finalizeStreamLifecycle(ctx, true)
		},
	})

	// Run processor
	err := processor.Run()

	// Track payload written for metrics collection
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}

	// Handle first token timeout specifically
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		_ = response.Body.Close()
		return ra.firstTokenTimeoutError()
	}

	// Check for context cancellation with first token timeout
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}

	return err
}

// handleStreamResponsePassthroughV2 uses StreamProcessor for unified passthrough handling.
// Works with any PassthroughCapable transformer (Anthropic, OpenAI Responses, etc.).
func (ra *relayAttempt) handleStreamResponsePassthroughV2(ctx context.Context, response *http.Response, cfg model.PassthroughConfig) error {
	defer ra.closeFirstTokenBudget()

	// Content-Type validation
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// Hand off early heartbeat
	ra.heartbeat.Hand()

	// Determine first token timeout
	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}

	semanticPayload := false
	observer := stream.NewIncrementalSSEObserver(maxSSEEventSize, cfg.TerminalEvents, func(ctx context.Context, _ string, data []byte) error {
		if len(data) == 0 {
			return nil
		}
		events, err := ra.outAdapter.TransformStreamEvent(ctx, data)
		if err != nil {
			ra.captureStreamError(err)
			return err
		}
		events, err = ra.ensureStreamFinalizer().ProcessStreamEvents(events)
		if err != nil {
			ra.captureStreamError(err)
			return err
		}
		if len(events) == 0 {
			return nil
		}
		if model.HasSemanticStreamEvents(events) {
			semanticPayload = true
		}
		_, err = ra.inAdapter.TransformStreamEvents(ctx, events)
		if err != nil {
			ra.captureStreamError(err)
		}
		return err
	})
	precommit := func(_, _ []byte) bool { return semanticPayload }
	precommitMaxEvents := maxSSEEventSize/(32*1024) + 8
	precommitMaxBytes := maxSSEEventSize
	const sseFramingAllowance = 64 * 1024
	if precommitMaxBytes <= int(^uint(0)>>1)-sseFramingAllowance {
		precommitMaxBytes += sseFramingAllowance
	}

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:             stream.NewRawSource(response.Body, 32*1024),
		Transform:          nil, // Passthrough: no transformation
		Observer:           observer,
		Writer:             ra.getStreamWriter(),
		Context:            ctx,
		FirstTokenTimeout:  firstTokenTimeout,
		HeartbeatInterval:  streamHeartbeatInterval(),
		PrecommitPredicate: precommit,
		PrecommitMaxEvents: precommitMaxEvents,
		PrecommitMaxBytes:  precommitMaxBytes,
		AllowEmptyPayload:  ra.allowEmptyPayload(),
		OnFirstToken: func() {
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.stopFirstTokenTimer()
		},
		OnFinish: func(ctx context.Context) error {
			if err := ra.finalizeStreamLifecycle(ctx, false); err != nil {
				return err
			}
			log.Debugf("passthrough stream end")
			return nil
		},
	})

	// Run processor
	err := processor.Run()

	// Track payload written for metrics collection
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}

	// Handle first token timeout specifically
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		_ = response.Body.Close()
		return ra.firstTokenTimeoutError()
	}

	// Check for context cancellation with first token timeout
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}

	return err
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, bool, error) {
	events, err := ra.outAdapter.TransformStreamEvent(ctx, []byte(data))
	if err != nil {
		ra.captureStreamError(err)
		log.Warnf("failed to transform stream events: %v", err)
		return nil, false, err
	}
	events, err = ra.ensureStreamFinalizer().ProcessStreamEvents(events)
	if err != nil {
		ra.captureStreamError(err)
		log.Warnf("failed to finalize stream events: %v", err)
		return nil, false, err
	}
	if len(events) == 0 {
		return nil, false, nil
	}
	semanticPayload := model.HasSemanticStreamEvents(events)
	inStream, err := ra.inAdapter.TransformStreamEvents(ctx, events)
	if err != nil {
		ra.captureStreamError(err)
		log.Warnf("failed to transform inbound stream events: %v", err)
		return nil, false, err
	}
	return inStream, semanticPayload, nil
}

func (ra *relayAttempt) ensureStreamFinalizer() *model.StreamFinalizer {
	if ra.streamFinalizer == nil {
		ra.streamFinalizer = model.NewStreamFinalizer()
	}
	return ra.streamFinalizer
}

func (ra *relayAttempt) captureStreamError(err error) {
	var responseError *model.ResponseError
	if errors.As(err, &responseError) {
		ra.upstreamError = responseError
	}
}

func (ra *relayAttempt) finalizeStreamLifecycle(ctx context.Context, writeTail bool) error {
	finalized, err := ra.ensureStreamFinalizer().FinalizeStream()
	if err != nil {
		ra.captureStreamError(err)
		return err
	}

	if len(finalized.TailEvents) > 0 {
		tail, transformErr := ra.inAdapter.TransformStreamEvents(ctx, finalized.TailEvents)
		if transformErr != nil {
			ra.captureStreamError(transformErr)
			return transformErr
		}
		if writeTail && len(tail) > 0 {
			writer := ra.getStreamWriter()
			if _, writeErr := writer.Write(tail); writeErr != nil {
				return fmt.Errorf("write stream finalization: %w", writeErr)
			}
			writer.Flush()
		}
	}

	if finalized.Response != nil && ra.metrics != nil && ra.responseCollected.CompareAndSwap(false, true) {
		actualModel := strings.TrimSpace(finalized.Response.Model)
		if actualModel == "" && ra.internalRequest != nil {
			actualModel = strings.TrimSpace(ra.internalRequest.Model)
		}
		ra.metrics.SetInternalResponse(finalized.Response, actualModel)
	}
	return nil
}

// handleResponse 处理非流式响应
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, response)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	// Empty response detection for non-streaming responses
	if ra.emptyResponseDetection {
		if err := validateNonStreamResponse(internalResponse); err != nil {
			log.Warnf("empty non-stream response detected from channel %s (model=%s)",
				ra.channel.Name, ra.internalRequest.Model)
			return err
		}
	}

	inResponse, err := ra.inAdapter.TransformResponse(ctx, internalResponse)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}
	if ctx != nil && ctx.Err() != nil {
		return contextError(ctx)
	}

	ra.c.Data(http.StatusOK, "application/json", inResponse)
	return nil
}

// collectResponse 收集响应信息
func (ra *relayAttempt) collectResponse() {
	if ra == nil || ra.inAdapter == nil || ra.metrics == nil {
		return
	}
	if !ra.responseCollected.CompareAndSwap(false, true) {
		return
	}
	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.requestContext())
	if err != nil {
		log.Debugf("collectResponse: failed to get internal response: %v", err)
		return
	}
	if internalResponse == nil {
		log.Debugf("collectResponse: internal response is nil (stream may not be complete)")
		return
	}

	actualModel := strings.TrimSpace(internalResponse.Model)
	if actualModel == "" && ra.internalRequest != nil {
		actualModel = strings.TrimSpace(ra.internalRequest.Model)
	}
	ra.metrics.SetInternalResponse(internalResponse, actualModel)
}

// forwardViaHTTPStandard 是 forwardViaHTTP 的原路径（直通判定失败时的兜底）。
// 留作显式出口，避免 passthrough 失败时的递归。
