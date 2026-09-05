package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
)

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
		!isLocalRelayBudgetError(fwdErr) &&
		!isFirstTokenTimeoutError(fwdErr) {
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
		op.ChannelKeyUpdateWithDelta(ra.usedKey, ra.metrics.Stats.InputCost+ra.metrics.Stats.OutputCost)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.breakerModelName())
		// Refresh model affinity only after the complete response succeeds.
		balancer.SetRoutingAffinity(ra.apiKeyID, ra.groupID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	if isClientCancellation(ra.requestContext(), fwdErr) {
		written := ra.streamPayloadWritten.Load()
		if written {
			ra.collectResponse()
		}
		op.ChannelKeyUpdateWithDelta(ra.usedKey, 0)
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
	op.ChannelKeyUpdateWithDelta(ra.usedKey, 0)
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
	firstTokenTimeout := isFirstTokenTimeoutError(fwdErr)
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

// runChannelAttempts owns the selected key reservation, including all exits
// during backoff, request preparation and budget checks.
func (r *httpRelay) runChannelAttempts(
	channel *dbmodel.Channel,
	usedKey dbmodel.ChannelKey,
	releaseKey func(),
	modelName string,
	decision outbound.CapabilityDecision,
) (result attemptResult, lastAttempt *relayRequest) {
	defer releaseKey()
	req := r.request
	ctx := req.c.Request.Context()
	isStream := req.internalRequest.Stream != nil && *req.internalRequest.Stream

	if budgetErr := r.budget.reserveChannel(channel.ID, time.Now()); budgetErr != nil {
		log.Warnf("relay failover budget exhausted: %v", budgetErr)
		return relayBudgetAttemptResult(budgetErr), nil
	}

	for attemptNum := 0; attemptNum < r.maxSameChannelAttempts; attemptNum++ {
		if attemptNum > 0 {
			// Avoid waiting for Retry-After when no attempt quota remains.
			if budgetErr := r.budget.attemptError(time.Now()); budgetErr != nil {
				log.Warnf("relay failover budget exhausted: %v", budgetErr)
				return relayBudgetAttemptResult(budgetErr), lastAttempt
			}
			delay := computeAttemptBackoff(attemptNum, result.RetryAt, result.RetryAfter)
			log.Infof("same-channel retry %d/%d for %s, waiting %v",
				attemptNum, r.maxSameChannelAttempts-1, channel.Name, delay)
			if waitErr := r.budget.wait(ctx, delay); waitErr != nil {
				if isLocalRelayBudgetError(waitErr) {
					log.Warnf("relay failover budget exhausted: %v", waitErr)
					return relayBudgetAttemptResult(waitErr), lastAttempt
				}
				log.Debugf("request context canceled during retry backoff")
				return attemptResult{Canceled: true, Err: waitErr}, lastAttempt
			}
		}

		attemptCtx := ctx
		cancelAttempt := func() {}
		if !isStream {
			attemptCtx, cancelAttempt = r.budget.attemptContext(attemptCtx)
		}
		attemptRequest, attemptErr := newAttemptRelayRequest(req, attemptCtx, modelName)
		if attemptErr != nil {
			cancelAttempt()
			classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to prepare relay attempt: %w", attemptErr))
			return attemptResult{
				Err:           classified,
				StatusCode:    http.StatusInternalServerError,
				Failure:       FailureClassification{Class: FailureConfiguration, StatusCode: http.StatusInternalServerError},
				ProtocolError: relayProtocolError(http.StatusInternalServerError, CodeRelayConfiguration, classified.Error()),
			}, lastAttempt
		}

		attempt := &relayAttempt{
			relayRequest:           attemptRequest,
			outAdapter:             outbound.Get(channel.Type),
			channel:                channel,
			usedKey:                usedKey,
			firstTokenTimeOutSec:   r.group.FirstTokenTimeOut,
			failoverDeadline:       r.budget.precommitDeadline(),
			emptyResponseDetection: r.emptyResponseDetection,
			capabilityDecision:     decision,
		}
		if budgetErr := r.budget.reserveAttempt(time.Now()); budgetErr != nil {
			cancelAttempt()
			log.Warnf("relay failover budget exhausted: %v", budgetErr)
			return relayBudgetAttemptResult(budgetErr), lastAttempt
		}

		result = attempt.attempt()
		cancelAttempt()
		lastAttempt = attemptRequest
		if result.EmptyResponse {
			log.Warnf("empty response from channel %s (model=%s, stream=%t, retry %d/%d)",
				channel.Name, modelName, isStream, attemptNum+1, r.maxSameChannelAttempts)
		}
		if result.Failure.Class == FailureBudgetExceeded {
			log.Warnf("relay failover budget exhausted: %v", result.Err)
			return relayBudgetAttemptResult(result.Err), lastAttempt
		}
		switchTime := time.Now()
		if !result.Written && !result.Canceled && !result.ResetConversation &&
			result.Failure.Class == FailureRateLimit &&
			req.iter.HasRemainingDifferentChannelMatching(channel.ID, r.rateLimitedChannels, func(candidateChannelID int) bool {
				return r.budget.canAttemptChannel(candidateChannelID, switchTime)
			}) {
			r.rateLimitedChannels[channel.ID] = struct{}{}
			log.Infof("channel %s rate limited; switching to a remaining channel without same-channel backoff", channel.Name)
			break
		}
		if result.Success || result.Written || result.Canceled || result.ResetConversation || result.FirstTokenTimeout || !result.Failure.Retryable {
			break
		}
	}
	return result, lastAttempt
}
