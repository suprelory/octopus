package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

// forwardViaWS sends at most one generation request. Status -1 permits HTTP
// fallback only when no WS generation request was sent by this attempt.
func (ra *relayAttempt) forwardViaWS(ctx context.Context) (int, error) {
	if ra.c == nil && effectiveResponsesWSMode(ra.channel) == responsesWSModePassthrough && !ra.internalRequest.IsOpenAIExactReplayRequest() && !channelParamOverrideActive(ra.channel) {
		return ra.forwardViaWSPassthrough(ctx)
	}
	preferredConnID := ""
	redial := ra.transportRecovery == upstreamRecoveryReconnect
	if requiresUpstreamWSContinuation(ra.internalRequest) && !redial {
		preferredConnID, _ = getWSResponseConn(currentPreviousResponseID(ra.internalRequest))
	}
	pc := TryUpstreamWSWithPreference(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), preferredConnID, redial)
	if pc == nil {
		return -1, nil
	}
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
	if err := ra.reserveSubmission(ctx); err != nil {
		wsUpstreamPool.Put(pc)
		return 0, err
	}
	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModeTransform)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	if redial {
		ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReconnect)
	}
	ra.upstreamTransport = "ws"
	if err := wsUpstreamPool.SendRaw(ctx, pc, reqBody); err != nil {
		wsUpstreamPool.RemoveConn(pc)
		return ra.upstreamWSFailure(ctx, 0, err, true)
	}
	reader := newWSUpstreamReader(pc, ra.channel.ID, ra.usedKey.ID)
	if err := ra.handleWSStreamResponseV2(ctx, reader); err != nil {
		ra.captureRetryAt(reader.RetryAt())
		reader.CloseWithError()
		return ra.upstreamWSFailure(ctx, reader.StatusCode(), err, false)
	}
	reader.Close()
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.recordSuccessfulWSAffinity(pc)
	return 200, nil
}

func isContinuationTransportFailure(err error) bool {
	if errors.Is(err, stream.ErrEmptyUpstreamStream) {
		return true
	}
	message := relayErrorMessage(err)
	return isUpstreamWSConnectionBroken(err) || needsConversationRestart(message) ||
		strings.Contains(message, "ws stream ended before first event")
}

func defaultWSModeForRequest(req *model.InternalLLMRequest) dbmodel.RelayLogWSMode {
	if requiresUpstreamWSContinuation(req) {
		return dbmodel.RelayLogWSModeContinuation
	}
	return dbmodel.RelayLogWSModeFresh
}
