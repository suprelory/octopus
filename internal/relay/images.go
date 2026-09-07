package relay

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/bodycache"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

// ImagesHandler 是 OpenAI Images API 的统一 relay 入口。
// endpoint 形如：/images/generations、/images/edits、/images/variations（不含 /v1 前缀）。
func ImagesHandler(endpoint string, c *gin.Context) {
	ctx := c.Request.Context()

	apiKeyID := c.GetInt("api_key_id")

	// 缓存请求体，支持多次重试重放
	bc, err := bodycache.New(c.Request.Body)
	if err != nil {
		var tooLarge *bodycache.BodyTooLargeError
		if errors.As(err, &tooLarge) {
			resp.Error(c, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() {
		if cerr := bc.Close(); cerr != nil {
			log.Warnf("failed to close images body cache: %v", cerr)
		}
	}()

	contentType := c.GetHeader("Content-Type")
	isMultipart := strings.Contains(strings.ToLower(contentType), "multipart/form-data")

	// 解析 requestModel 与 stream（严格模式：model 必填）
	var (
		requestModel string
		stream       bool
		boundary     string
		jsonPayload  map[string]any
	)
	if isMultipart {
		_, params, perr := mime.ParseMediaType(contentType)
		if perr != nil {
			resp.Error(c, http.StatusBadRequest, "invalid multipart content-type")
			return
		}
		boundary = strings.TrimSpace(params["boundary"])
		if boundary == "" {
			resp.Error(c, http.StatusBadRequest, "invalid multipart boundary")
			return
		}
		m, s, perr := parseMultipartModelAndStream(bc, boundary)
		if perr != nil {
			resp.Error(c, http.StatusBadRequest, perr.Error())
			return
		}
		requestModel = m
		stream = s
	} else {
		payload, m, s, perr := parseJSONModelAndStream(bc)
		if perr != nil {
			resp.Error(c, http.StatusBadRequest, perr.Error())
			return
		}
		jsonPayload = payload
		requestModel = m
		stream = s
	}

	// supported_models 校验（复用 APIKeyAuth 注入）
	supportedModels := strings.TrimSpace(c.GetString("supported_models"))
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, requestModel) {
			resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
			return
		}
	}

	// 获取通道分组
	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}
	candidateSnapshot := newCandidateSnapshot(ctx, group)

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}
	defer iter.Close()

	// 初始化 Metrics（Images 独立，避免 b64_json 内存膨胀）
	metrics := newImagesRelayMetrics(apiKeyID, requestModel, middleware.ClientIP(c))
	metrics.RequestContent = buildImagesRequestContentForLog(isMultipart, bc, jsonPayload)

	// === 早期心跳 ===
	// 流式：启动早期心跳协程，覆盖前置阶段（连接慢、failover、退避）期间向客户端发 SSE 注释字节
	// 非流式：无法发送 SSE 注释（破坏 application/json 协议），不施加本地超时
	hb := startEarlyHeartbeat(c, stream)
	defer hb.Stop()

	var lastErr error
	var capabilityErr error
	var sawSupportedCapability bool
	var capabilityErrorCode string
	var capabilityErrorMessage string
	var lastRetryAt time.Time
	var lastFailure FailureClassification
	capabilityPolicy := getCapabilityDegradationPolicy()

	for iter.Next() {
		select {
		case <-ctx.Done():
			log.Debugf("request context canceled, stopping retry")
			metrics.SaveWithChannelStats(ctx, false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()

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

		decision := outbound.PlanRelayOperation(channel.Type, outbound.RelayOperationImages)
		decorateParamOverrideDecision(&decision, helper.InspectParamOverride(channel.ParamOverride), channelParamOverrideActive(channel))
		logRelayCapability(channel, item.ModelName, decision, capabilityPolicy)
		if reject, errorCode := evaluateCapabilityPolicy(decision, capabilityPolicy); reject {
			message := capabilityRejectionMessage(decision, channel.Type.String())
			iter.SkipWithCapability(channel.ID, 0, channel.Name, message, capabilityTrace(decision, capabilityPolicy, channel.Type.String()))
			capabilityErr = fmt.Errorf("capability rejected: %s", message)
			capabilityErrorCode = errorCode
			capabilityErrorMessage = message
			continue
		}
		sawSupportedCapability = true

		excludedKeyIDs := make(map[int]struct{})
		usedKey, releaseKey := selectAndReserveRelayKey(iter, channel, excludedKeyIDs)
		if usedKey.ChannelKey == "" {
			if len(excludedKeyIDs) == 0 {
				iter.Skip(channel.ID, 0, channel.Name, "no available key")
			} else {
				iter.InvalidateCurrentPreference()
			}
			continue
		}

		log.Debugf("images request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t, stream=%t)",
			requestModel, group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky(), stream)

		span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
		span.SetCapability(capabilityTrace(decision, capabilityPolicy, channel.Type.String()))

		// 尝试一次转发
		var retryAt time.Time
		statusCode, written, usage, upstreamCT, fwdErr := imagesAttempt(ctx, endpoint, c, bc, isMultipart, boundary, jsonPayload, stream, channel, usedKey.ChannelKey, group.FirstTokenTimeOut, metrics, item.ModelName, hb, &retryAt)
		releaseKey()

		// 更新 channel key 状态
		usedKey.StatusCode = statusCode
		usedKey.LastUseTimeStamp = time.Now().Unix()

		if fwdErr == nil {
			// ====== 成功 ======
			actualModel := strings.TrimSpace(metrics.ActualModel)
			if actualModel == "" {
				actualModel = item.ModelName
				metrics.ActualModel = actualModel
			}
			if usage != nil {
				metrics.SetUsageFromImages(actualModel, *usage)
			}
			metrics.ResponseContent = buildImagesResponseContentForLog(stream, upstreamCT, usage)

			op.ChannelKeyUpdateWithDelta(usedKey, metrics.Stats.InputCost+metrics.Stats.OutputCost)

			span.End(model.AttemptSuccess, statusCode, "")

			// Channel 维度统计
			op.StatsChannelUpdate(channel.ID, model.StatsMetrics{
				WaitTime:       span.Duration().Milliseconds(),
				RequestSuccess: 1,
			})

			// 熔断器：记录成功
			balancer.RecordSuccess(channel.ID, usedKey.ID, item.ModelName)
			// Refresh affinity only after the complete image response succeeds.
			balancer.SetRoutingAffinity(apiKeyID, group.ID, requestModel, channel.ID, usedKey.ID)

			metrics.SaveWithChannelStats(ctx, true, nil, iter.Attempts(), false)
			return
		}

		// ====== 失败 ======
		failure := classifyRelayFailureContext(ctx, statusCode, fwdErr, retryAt)
		op.ChannelKeyUpdateWithDelta(usedKey, 0)
		span.SetFailure(string(failure.Class), failure.Retryable, failure.RetryAt)
		span.End(model.AttemptFailed, statusCode, fwdErr.Error())

		// Channel 维度统计
		op.StatsChannelUpdate(channel.ID, model.StatsMetrics{
			WaitTime:      span.Duration().Milliseconds(),
			RequestFailed: 1,
		})

		// 熔断器：只记录可归因于上游的失败；请求错误和客户端取消不污染渠道状态。
		if failure.Record {
			retryAt = recordFailureAndResolveRetryAt(channel.ID, usedKey.ID, item.ModelName, failure, retryAt)
			failure.RetryAt = retryAt
		}

		if written {
			metrics.SaveWithChannelStats(ctx, false, fwdErr, iter.Attempts(), false)
			return
		}

		iter.InvalidateCurrentPreference()
		lastErr = fmt.Errorf("channel %s failed: %w", channel.Name, fwdErr)
		lastRetryAt = retryAt
		lastFailure = failure
	}

	// 所有通道都失败
	finalErr := lastErr
	if !sawSupportedCapability && capabilityErr != nil {
		finalErr = capabilityErr
	}
	metrics.SaveWithChannelStats(ctx, false, finalErr, iter.Attempts(), false)
	if !sawSupportedCapability && capabilityErrorCode != "" {
		if hb.Handoff() {
			hb.WriteSSEError(http.StatusBadRequest, capabilityErrorMessage)
		} else {
			resp.ErrorWithCode(c, http.StatusBadRequest, capabilityErrorCode, capabilityErrorMessage)
		}
		return
	}
	if lastFailure.Passthrough {
		if value := retryAfterHeaderValue(lastRetryAt, time.Now()); value != "" {
			c.Header("Retry-After", value)
		}
		status := lastFailure.StatusCode
		if status < 400 {
			status = http.StatusBadGateway
		}
		writeImagesFailure(c, hb, attemptResult{
			Err:        finalErr,
			StatusCode: status,
			RetryAt:    lastRetryAt,
			Failure:    lastFailure,
		}, finalErr)
		return
	}
	writeImagesFailure(c, hb, attemptResult{
		Err:        finalErr,
		StatusCode: lastFailure.StatusCode,
		RetryAt:    lastRetryAt,
		Failure:    lastFailure,
	}, finalErr)
}

func writeImagesFailure(c *gin.Context, hb *earlyHeartbeat, result attemptResult, err error) {
	responseError := protocolErrorForAttempt(result, err)
	if responseError == nil {
		responseError = relayProtocolError(http.StatusBadGateway, CodeRelayUpstreamFailed, "all channels failed")
	}
	if hb != nil && hb.Handoff() {
		hb.WriteSSEError(responseError.StatusCode, responseError.Detail.Message)
		return
	}
	resp.ErrorWithCode(c, responseError.StatusCode, responseError.Detail.Code, responseError.Detail.Message)
}
