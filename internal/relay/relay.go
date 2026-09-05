package relay

import (
	"context"
	"fmt"
	"net/http"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

// httpRelay owns the candidate/retry policy for one inbound HTTP request.
// Each upstream attempt still receives its own relayRequest and adapters.
type httpRelay struct {
	request                *relayRequest
	group                  dbmodel.Group
	replayState            *wsConversationState
	emptyResponseDetection bool
	maxSameChannelAttempts int
	budget                 *relayFailoverBudget
	rateLimitedChannels    map[int]struct{}
}

func Handler(inboundType inbound.InboundType, c *gin.Context) {
	relay := prepareHTTPRelay(inboundType, c)
	if relay == nil {
		return
	}
	defer relay.request.iter.Close()
	defer relay.request.heartbeat.Stop()

	relay.run()
}

func (r *httpRelay) run() {
	req := r.request
	c, iter, metrics := req.c, req.iter, req.metrics
	var lastErr error
	var lastResult attemptResult
	var lastAttempt *relayRequest
	var capabilityErr error
	var capabilityResult attemptResult
	var sawSupportedCapability bool

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Debugf("request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}
		if budgetErr := r.budget.timeError(time.Now()); budgetErr != nil {
			lastErr = budgetErr
			lastResult = relayBudgetAttemptResult(budgetErr)
			log.Warnf("relay failover budget exhausted: %v", budgetErr)
			break
		}

		item := iter.Item()
		if _, rateLimited := r.rateLimitedChannels[item.ChannelID]; rateLimited {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), "channel rate limited earlier in this request")
			continue
		}

		channel, err := req.candidateSnapshot.Channel(item.ChannelID)
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

		// Validate the type before selecting a key. Actual attempts get fresh adapters.
		candidateAdapter := outbound.Get(channel.Type)
		if candidateAdapter == nil {
			iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}
		decision := planRelayCapability(req, channel, candidateAdapter, item.ModelName)
		logRelayCapability(channel, item.ModelName, decision, req.capabilityPolicy)
		if reject, errorCode := evaluateCapabilityPolicy(decision, req.capabilityPolicy); reject {
			message := capabilityRejectionMessage(decision, channel.Type.String())
			iter.SkipWithCapability(channel.ID, 0, channel.Name, message, capabilityTrace(decision, req.capabilityPolicy, channel.Type.String()))
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

		log.Debugf("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
			req.requestModel, r.group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky())

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

		result, attempt := r.runChannelAttempts(channel, usedKey, releaseKey, item.ModelName, decision)
		if attempt != nil {
			lastAttempt = attempt
		}
		if result.Failure.Class == FailureBudgetExceeded {
			lastErr, lastResult = result.Err, result
			break
		}

		// Record the breaker failure only after all retries on this channel finish.
		if !result.Success && !result.Written && !result.Canceled && !result.ResetConversation && result.Failure.Record {
			failureKind := failureCircuitKind(result.Failure)
			result.RetryAt = recordFailureAndResolveRetryAt(channel.ID, usedKey.ID, item.ModelName, result.Failure, result.RetryAt)
			result.Failure.RetryAt = result.RetryAt
			if failureKind == balancer.FailureTransient {
				maybeLearnManagedRoute(c.Request.Context(), channel.ID, item.ModelName, req.inboundType, result.Err)
			}
		}

		if result.Success {
			r.saveResponsesReplay(lastAttempt, channel, usedKey)
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
				writeInboundProtocolError(c, req.heartbeat, errorAdapter, relayProtocolError(publicErr.Status, CodeRelayUpstreamFailed, publicErr.Message))
			} else {
				writeInboundProtocolError(c, req.heartbeat, errorAdapter, protocolErrorFromError(result.StatusCode, result.Err))
			}
			return
		}
		if result.Written {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			return
		}
		iter.InvalidateCurrentPreference()
		lastErr, lastResult = result.Err, result
	}

	// Capability rejection must not mask a failure from an acceptable candidate.
	lastResult, lastErr = resolveFinalAttemptResult(
		sawSupportedCapability, lastErr, lastResult, capabilityErr, capabilityResult,
	)
	metrics.SaveWithChannelStats(c.Request.Context(), false, lastErr, iter.Attempts(), false)
	r.writeFinalError(lastResult, lastErr, lastAttempt)
}
