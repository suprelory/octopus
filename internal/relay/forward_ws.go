package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
)

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
				balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.groupID, ra.requestModel)
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
			balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.groupID, ra.requestModel)
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
			balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.groupID, ra.requestModel)
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
			balancer.DeleteRoutingAffinity(ra.apiKeyID, ra.groupID, ra.requestModel)
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

func defaultWSModeForRequest(req *model.InternalLLMRequest) dbmodel.RelayLogWSMode {
	if requiresUpstreamWSContinuation(req) {
		return dbmodel.RelayLogWSModeContinuation
	}
	return dbmodel.RelayLogWSModeFresh
}
