package relay

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

type wsRelayResult struct {
	Success           bool
	ResponseID        string
	ResetConversation bool
	Written           bool
	Canceled          bool
	Err               error
	Failure           FailureClassification
	RetryAt           time.Time
	PublicError       *wsPublicError
	Attempt           *relayRequest
}

func runWSRelay(ctx context.Context, req *relayRequest, group *dbmodel.Group, emptyResponseDetection bool) wsRelayResult {
	if req == nil || req.iter == nil {
		return wsRelayResult{Err: fmt.Errorf("relay request iterator is nil")}
	}
	defer req.iter.Close()
	replayExact := req != nil && req.internalRequest != nil && req.internalRequest.IsOpenAIExactReplayRequest()
	relayCtx := ctx
	if replayExact {
		budget := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline); remaining > 0 && remaining < budget {
				budget = remaining
			}
		}
		var cancel context.CancelFunc
		relayCtx, cancel = context.WithTimeoutCause(ctx, budget, errLocalRelayBudgetExceeded)
		defer cancel()
	}

	maxSameChannelAttempts := sameChannelMaxAttempts(group.RetryEnabled, group.MaxRetries)
	capabilityPolicy := getCapabilityDegradationPolicy()
	req.capabilityPolicy = capabilityPolicy

	var lastErr error
	var lastResult attemptResult
	var lastAttempt *relayRequest
	var capabilityErr error
	var capabilityResult attemptResult
	var sawSupportedCapability bool
	rateLimitedChannels := make(map[int]struct{})
	maxChannelAttempts := req.iter.Len()
	if replayExact && maxChannelAttempts > 3 {
		maxChannelAttempts = 3
	}

	for req.iter.Next() {
		if req.iter.Index() >= maxChannelAttempts {
			break
		}
		select {
		case <-relayCtx.Done():
			if isLocalRelayBudgetExceeded(relayCtx, contextError(relayCtx)) {
				publicErr := wsPublicError{
					Status:  http.StatusGatewayTimeout,
					Code:    "replay_recovery_timeout",
					Message: "exact replay 恢复超过本地 15 秒预算，请重试",
				}
				return wsRelayResult{Err: contextError(relayCtx), PublicError: &publicErr, Attempt: lastAttempt}
			}
			return wsRelayResult{Canceled: true, Err: relayCtx.Err(), Attempt: lastAttempt}
		default:
		}

		item := req.iter.Item()
		if _, rateLimited := rateLimitedChannels[item.ChannelID]; rateLimited {
			req.iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), "channel rate limited earlier in this request")
			continue
		}

		channel, err := req.candidateSnapshot.Channel(item.ChannelID)
		if err != nil {
			req.iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			req.iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		candidateAdapter := outbound.Get(channel.Type)
		if candidateAdapter == nil {
			req.iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		decision := planRelayCapability(req, channel, candidateAdapter, item.ModelName)
		logRelayCapability(channel, item.ModelName, decision, capabilityPolicy)
		if reject, errorCode := evaluateCapabilityPolicy(decision, capabilityPolicy); reject {
			message := capabilityRejectionMessage(decision, channel.Type.String())
			req.iter.SkipWithCapability(channel.ID, 0, channel.Name, message, capabilityTrace(decision, capabilityPolicy, channel.Type.String()))
			candidateErr := fmt.Errorf("capability rejected: %s", message)
			candidateResult := attemptResult{
				Err:           candidateErr,
				StatusCode:    http.StatusBadRequest,
				ProtocolError: relayProtocolError(http.StatusBadRequest, errorCode, message),
			}
			capabilityResult, capabilityErr = preferCapabilityRejection(capabilityErr, capabilityResult, candidateErr, candidateResult)
			continue
		}
		sawSupportedCapability = true

		excludedKeyIDs := make(map[int]struct{})
		usedKey, releaseKey := selectAndReserveRelayKey(req.iter, channel, excludedKeyIDs)
		if usedKey.ChannelKey == "" {
			if len(excludedKeyIDs) == 0 {
				req.iter.Skip(channel.ID, 0, channel.Name, "no available key")
			} else {
				req.iter.InvalidateCurrentPreference()
			}
			continue
		}

		log.Debugf("ws request model %s, forwarding to channel: %s model: %s (attempt %d/%d)",
			req.requestModel, channel.Name, item.ModelName, req.iter.Index()+1, req.iter.Len())

		var result attemptResult
		for attemptNum := 0; attemptNum < maxSameChannelAttempts; attemptNum++ {
			if attemptNum > 0 {
				delay := computeAttemptBackoff(attemptNum, result.RetryAt, result.RetryAfter)
				if !waitBackoff(relayCtx, delay) {
					releaseKey()
					if isLocalRelayBudgetExceeded(relayCtx, contextError(relayCtx)) {
						publicErr := wsPublicError{
							Status:  http.StatusGatewayTimeout,
							Code:    "replay_recovery_timeout",
							Message: "exact replay 恢复超过本地 15 秒预算，请重试",
						}
						return wsRelayResult{Err: contextError(relayCtx), PublicError: &publicErr, Attempt: lastAttempt}
					}
					return wsRelayResult{Canceled: true, Err: relayCtx.Err(), Attempt: lastAttempt}
				}
			}

			attemptRequest, attemptErr := newAttemptRelayRequest(req, relayCtx, item.ModelName)
			if attemptErr != nil {
				classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to prepare relay attempt: %w", attemptErr))
				result = attemptResult{
					Err:           classified,
					StatusCode:    http.StatusInternalServerError,
					Failure:       FailureClassification{Class: FailureConfiguration, StatusCode: http.StatusInternalServerError},
					ProtocolError: relayProtocolError(http.StatusInternalServerError, CodeRelayConfiguration, classified.Error()),
				}
				break
			}

			ra := &relayAttempt{
				relayRequest:           attemptRequest,
				outAdapter:             outbound.Get(channel.Type),
				channel:                channel,
				usedKey:                usedKey,
				firstTokenTimeOutSec:   group.FirstTokenTimeOut,
				emptyResponseDetection: emptyResponseDetection,
				capabilityDecision:     decision,
			}

			result = ra.attempt()
			lastAttempt = attemptRequest
			if !result.Written && !result.Canceled && !result.ResetConversation &&
				result.Failure.Class == FailureRateLimit &&
				req.iter.HasRemainingDifferentChannelExcept(channel.ID, rateLimitedChannels) {
				rateLimitedChannels[channel.ID] = struct{}{}
				break
			}
			if result.Success || result.Written || result.Canceled || result.ResetConversation || !result.Failure.Retryable {
				break
			}
		}
		releaseKey()

		if !result.Success && !result.Canceled && !result.ResetConversation && result.Failure.Record {
			result.RetryAt = recordFailureAndResolveRetryAt(channel.ID, usedKey.ID, item.ModelName, result.Failure, result.RetryAt)
			result.Failure.RetryAt = result.RetryAt
			if !result.Written {
				req.iter.InvalidateCurrentPreference()
			}
		}

		if result.Success {
			var respID string
			if req.metrics.InternalResponse != nil {
				respID = req.metrics.InternalResponse.ID
			}
			return wsRelayResult{Success: true, ResponseID: respID, Attempt: lastAttempt}
		}
		if result.ResetConversation {
			if publicErr, ok := classifyWSPublicError(result.Err, result.StatusCode); ok {
				return wsRelayResult{ResetConversation: publicErr.ResetConversation, Err: result.Err, Failure: result.Failure, RetryAt: result.RetryAt, PublicError: &publicErr, Attempt: lastAttempt}
			}
			return wsRelayResult{ResetConversation: true, Err: result.Err, Failure: result.Failure, RetryAt: result.RetryAt, Attempt: lastAttempt}
		}
		if result.Canceled || result.Written {
			return wsRelayResult{Written: result.Written, Canceled: result.Canceled, Err: result.Err, Failure: result.Failure, RetryAt: result.RetryAt, Attempt: lastAttempt}
		}
		lastErr = result.Err
		lastResult = result
	}

	lastResult, lastErr = resolveFinalAttemptResult(
		sawSupportedCapability,
		lastErr,
		lastResult,
		capabilityErr,
		capabilityResult,
	)
	if lastResult.StatusCode == http.StatusBadRequest && lastResult.ProtocolError != nil {
		code := lastResult.ProtocolError.Detail.Code
		if code == CodeRelayModelNotSupported || code == CodeRelayCapabilityRejected {
			publicErr := wsPublicError{Status: http.StatusBadRequest, Code: code, Message: lastResult.ProtocolError.Detail.Message}
			return wsRelayResult{Err: lastErr, Failure: lastResult.Failure, RetryAt: lastResult.RetryAt, PublicError: &publicErr, Attempt: lastAttempt}
		}
	}
	if publicErr, ok := classifyWSPublicError(lastErr, lastResult.StatusCode); ok {
		return wsRelayResult{ResetConversation: publicErr.ResetConversation, Err: lastErr, Failure: lastResult.Failure, RetryAt: lastResult.RetryAt, PublicError: &publicErr, Attempt: lastAttempt}
	}
	return wsRelayResult{Err: lastErr, Failure: lastResult.Failure, RetryAt: lastResult.RetryAt, Attempt: lastAttempt}
}

func finalizeWSRelay(ctx context.Context, conn *websocket.Conn, req *relayRequest, result wsRelayResult) wsRelayResult {
	if result.Success {
		req.metrics.SaveWithChannelStats(ctx, true, nil, req.iter.Attempts(), false)
		return result
	}

	req.metrics.SaveWithChannelStats(ctx, false, result.Err, req.iter.Attempts(), false)
	if result.Canceled || result.Written {
		return result
	}
	if result.PublicError != nil {
		if result.PublicError.ResetConversation {
			balancer.DeleteRoutingAffinity(req.apiKeyID, req.groupID, req.requestModel)
		}
		writeWSError(ctx, conn, result.PublicError.Status, result.PublicError.Code, result.PublicError.Message, result.RetryAt)
		return result
	}
	if result.Failure.Class != FailureNone {
		status, code := defaultFailureProtocol(result.Failure.Class, result.Failure.StatusCode)
		message := "All channels failed"
		if result.Err != nil && strings.TrimSpace(result.Err.Error()) != "" {
			message = result.Err.Error()
		}
		writeWSError(ctx, conn, status, code, message, result.RetryAt)
		return result
	}
	writeWSError(ctx, conn, 502, "all_channels_failed", "All channels failed")
	return result
}

func finalChannelKey(attempts []dbmodel.ChannelAttempt) (int, int) {
	var lastChannelID int
	var lastChannelKeyID int
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		if attempt.Status == dbmodel.AttemptSuccess {
			return attempt.ChannelID, attempt.ChannelKeyID
		}
		if attempt.Status == dbmodel.AttemptFailed && lastChannelID == 0 {
			lastChannelID = attempt.ChannelID
			lastChannelKeyID = attempt.ChannelKeyID
		}
	}
	return lastChannelID, lastChannelKeyID
}
