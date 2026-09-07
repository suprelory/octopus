package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

func processWSResponseCreate(
	ctx context.Context,
	conn *websocket.Conn,
	data []byte,
	apiKeyID int,
	supportedModels string,
	clientIP string,
	downstreamSessionID string,
	conversationState *wsConversationState,
) *wsConversationState {
	var reqBody map[string]json.RawMessage
	if err := json.Unmarshal(data, &reqBody); err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse request body")
		return conversationState
	}

	// Remove WS-only fields
	delete(reqBody, "type")
	requestModel := strings.TrimSpace(extractWSRequestModel(reqBody))
	allowStoredRestore := wsRequestExplicitlyRequestsContinuation(reqBody)
	requestedPreviousResponseID := ""
	if raw, ok := reqBody["previous_response_id"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &requestedPreviousResponseID)
		requestedPreviousResponseID = strings.TrimSpace(requestedPreviousResponseID)
	}
	hadLocalState := conversationState != nil
	conversationState = resolveWSConversationState(apiKeyID, requestModel, conversationState, allowStoredRestore, downstreamSessionID)
	hasResolvedState := conversationState != nil
	resolvedLastResponseID := ""
	if conversationState != nil {
		resolvedLastResponseID = strings.TrimSpace(conversationState.LastResponseID)
	}
	log.Debugf("ws response.create state resolved (apikey=%d, request_model=%s, requested_prev=%s, explicit_continuation=%t, had_local_state=%t, resolved_state=%t, resolved_last_response_id=%s)",
		apiKeyID, requestModel, requestedPreviousResponseID, allowStoredRestore, hadLocalState, hasResolvedState, resolvedLastResponseID)
	if conversationState != nil {
		conversationState.DownstreamSessionID = downstreamSessionID
	}
	rewriteWSPreviousResponseID(reqBody, conversationState)
	preferredSticky := wsConversationStateToSticky(conversationState)
	if preferredSticky == nil && requestedPreviousResponseID != "" {
		if group, err := op.GroupGetEnabledMap(requestModel, ctx); err == nil {
			scope := wsAffinityScope{APIKeyID: apiKeyID, GroupID: group.ID, RequestModel: requestModel, ResponseID: requestedPreviousResponseID}
			if entry, ok := getWSAffinityStore().Get(ctx, scope); ok {
				preferredSticky = &balancer.SessionEntry{ChannelID: entry.ChannelID, ChannelKeyID: entry.ChannelKeyID, Timestamp: time.Now()}
				log.Debugf("ws response affinity hit (apikey=%d, group=%d, request_model=%s, previous_response_id=%s, channel=%d, key=%d)",
					apiKeyID, group.ID, requestModel, requestedPreviousResponseID, entry.ChannelID, entry.ChannelKeyID)
			}
		}
	}

	// Check for generate: false (warmup). Codex-style clients use this as a
	// prewarm probe and do not expect a synthetic completed response turn.
	// Acknowledging it locally caused some clients to wait forever for a normal
	// response lifecycle. Prime the upstream pool best-effort, then stay silent.
	if genRaw, ok := reqBody["generate"]; ok {
		var generate bool
		if json.Unmarshal(genRaw, &generate) == nil && !generate {
			if err := bestEffortWarmupUpstreamWS(ctx, apiKeyID, supportedModels, reqBody); err != nil {
				log.Warnf("ws warmup failed (apikey=%d): %v", apiKeyID, err)
			} else {
				log.Debugf("ws warmup ready (apikey=%d)", apiKeyID)
			}
			return conversationState
		}
		delete(reqBody, "generate")
	}

	// Force stream mode
	reqBody["stream"] = json.RawMessage("true")

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeWSError(ctx, conn, 500, "server_error", "Failed to build request")
		return conversationState
	}

	// Parse request
	inAdapter := inbound.Get(inbound.InboundTypeOpenAIResponse)
	internalRequest, err := inAdapter.TransformRequest(ctx, bodyBytes)
	if err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", err.Error())
		return conversationState
	}
	originalRequest := cloneInternalRequest(internalRequest)
	continuationRequested := allowStoredRestore || requestContainsToolOutputs(originalRequest)
	if !continuationRequested {
		deleteWSConversationState(apiKeyID, requestModel, downstreamSessionID)
		conversationState = nil
		preferredSticky = nil
	}
	executionRequest := originalRequest
	if conversationState != nil && continuationRequested {
		if conversationState.ShouldUseNativeContinuation(originalRequest) {
			log.Debugf("ws relay using native continuation (apikey=%d, request_model=%s, previous_response_id=%s)", apiKeyID, requestModel, currentPreviousResponseID(originalRequest))
		} else if conversationState.ShouldUseLocalReplay(originalRequest) {
			replayedRequest := conversationState.BuildReplayRequest(originalRequest)
			if replayedRequest != nil {
				executionRequest = replayedRequest
			}
		}
	}

	// Check supported models
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		found := false
		for _, m := range supportedModelsArray {
			if m == executionRequest.Model {
				found = true
				break
			}
		}
		if !found {
			writeWSError(ctx, conn, 400, "invalid_request", "model not supported")
			return conversationState
		}
	}

	requestModel = executionRequest.Model
	emptyResponseDetection := emptyResponseDetectionEnabled()
	req, group, err := newWSRelayRequest(ctx, conn, inAdapter, apiKeyID, requestModel, clientIP, cloneInternalRequest(executionRequest), originalRequest, preferredSticky, bodyBytes)
	if err != nil {
		status := 404
		code := "model_not_found"
		if err.Error() == "no available channel" {
			status = 503
			code = "no_available_channel"
		}
		writeWSError(ctx, conn, status, code, err.Error())
		return conversationState
	}

	autoRestart := conversationState != nil && continuationRequested && conversationState.CanAutoRestart(originalRequest)
	failedPreviousResponseID := currentPreviousResponseID(originalRequest)
	log.Debugf("ws relay prepared (apikey=%d, request_model=%s, previous_response_id=%s, auto_replay=%t, preferred_channel=%d, preferred_key=%d)",
		apiKeyID, requestModel, failedPreviousResponseID, autoRestart,
		func() int {
			if preferredSticky == nil {
				return 0
			}
			return preferredSticky.ChannelID
		}(),
		func() int {
			if preferredSticky == nil {
				return 0
			}
			return preferredSticky.ChannelKeyID
		}())
	result := runWSRelay(ctx, req, group, emptyResponseDetection)
	if result.Attempt != nil {
		req = result.Attempt
	}
	if result.ResetConversation && autoRestart && !req.streamWriter.Written() {
		log.Debugf("ws relay switching to replay (apikey=%d, request_model=%s, failed_previous_response_id=%s, reset_conversation=%t)",
			apiKeyID, requestModel, failedPreviousResponseID, result.ResetConversation)
		balancer.DeleteRoutingAffinity(apiKeyID, group.ID, requestModel)
		replayedRequest := conversationState.BuildReplayRequest(originalRequest)
		replayReq, replayGroup, replayErr := newWSRelayRequest(ctx, conn, inAdapter, apiKeyID, requestModel, clientIP, replayedRequest, originalRequest, preferredSticky, bodyBytes)
		if replayErr == nil {
			replayReq.metrics.SetWSMode(dbmodel.RelayLogWSModeReplay)
			replayReq.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReplay)
			req = replayReq
			group = replayGroup
			result = runWSRelay(ctx, req, group, emptyResponseDetection)
			if result.Attempt != nil {
				req = result.Attempt
			}
		}
	}

	result = finalizeWSRelay(ctx, conn, req, result)
	if result.Success {
		if conversationState == nil {
			conversationState = &wsConversationState{DownstreamSessionID: downstreamSessionID}
		}
		conversationState.DownstreamSessionID = downstreamSessionID
		if channelID, keyID := finalChannelKey(req.iter.Attempts()); channelID > 0 {
			conversationState.ChannelID = channelID
			conversationState.ChannelKeyID = keyID
		}
		if req.metrics.WSMode != nil && *req.metrics.WSMode == dbmodel.RelayLogWSModeReplay {
			conversationState.RememberReplayAlias(failedPreviousResponseID)
			conversationState.MarkReplayRecovered(originalRequest)
		} else {
			conversationState.MarkNativeContinuationReady()
		}
		conversationState.ApplySuccessfulTurn(originalRequest, req.metrics.InternalResponse)
		storeWSConversationState(apiKeyID, requestModel, conversationState, wsConversationStateTTL(group.SessionKeepTime))
		log.Debugf("ws relay success state stored (apikey=%d, request_model=%s, ws_mode=%v, ws_recovery=%v, last_response_id=%s, channel=%d, key=%d)",
			apiKeyID, requestModel, req.metrics.WSMode, req.metrics.WSRecovery,
			strings.TrimSpace(conversationState.LastResponseID), conversationState.ChannelID, conversationState.ChannelKeyID)
		return conversationState
	}
	if result.ResetConversation {
		log.Debugf("ws relay clearing conversation state (apikey=%d, request_model=%s, err=%v)", apiKeyID, requestModel, result.Err)
		deleteWSConversationState(apiKeyID, requestModel, downstreamSessionID)
		return nil
	}

	return conversationState
}

func extractWSRequestModel(reqBody map[string]json.RawMessage) string {
	if len(reqBody) == 0 {
		return ""
	}
	modelRaw, ok := reqBody["model"]
	if !ok {
		return ""
	}
	var requestModel string
	if err := json.Unmarshal(modelRaw, &requestModel); err != nil {
		return ""
	}
	return requestModel
}

func newWSRelayRequest(
	ctx context.Context,
	conn *websocket.Conn,
	inAdapter transformerModel.Inbound,
	apiKeyID int,
	requestModel string,
	clientIP string,
	executionRequest *transformerModel.InternalLLMRequest,
	metricsRequest *transformerModel.InternalLLMRequest,
	preferredSticky *balancer.SessionEntry,
	rawBody []byte,
) (*relayRequest, *dbmodel.Group, error) {
	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("model not found")
	}
	candidateSnapshot := newCandidateSnapshot(ctx, group)

	capabilityPlanner := newRelayCapabilityPlanner(executionRequest, rawBody, true)
	iter := balancer.NewIteratorWithPreferenceAndQuality(group, apiKeyID, requestModel, preferredSticky, func(item dbmodel.GroupItem) int {
		channel, _ := candidateSnapshot.Channel(item.ChannelID)
		return capabilityPlanner.rankChannel(channel, item)
	})
	if iter.Len() == 0 {
		return nil, nil, fmt.Errorf("no available channel")
	}

	return &relayRequest{
		c:                 nil,
		ctx:               ctx,
		inAdapter:         inAdapter,
		inboundType:       inbound.InboundTypeOpenAIResponse,
		internalRequest:   executionRequest,
		metrics:           NewRelayMetrics(apiKeyID, requestModel, "responses", clientIP, rawBody, metricsRequest),
		apiKeyID:          apiKeyID,
		requestModel:      requestModel,
		groupID:           group.ID,
		groupSessionTTL:   group.SessionKeepTime,
		iter:              iter,
		capabilityPlanner: capabilityPlanner,
		candidateSnapshot: candidateSnapshot,
		rawBody:           rawBody,
		streamWriter:      NewWSStreamWriter(ctx, conn),
	}, &group, nil
}

func rewriteWSPreviousResponseID(reqBody map[string]json.RawMessage, state *wsConversationState) {
	if state == nil || len(reqBody) == 0 {
		return
	}
	raw, ok := reqBody["previous_response_id"]
	if !ok || len(raw) == 0 {
		return
	}
	var previousResponseID string
	if err := json.Unmarshal(raw, &previousResponseID); err != nil {
		return
	}
	if !state.ShouldRewritePreviousResponseID(previousResponseID) {
		return
	}
	reqBody["previous_response_id"] = json.RawMessage(fmt.Sprintf("%q", state.LastResponseID))
}

func currentPreviousResponseID(req *transformerModel.InternalLLMRequest) string {
	return req.OpenAIPreviousResponseID()
}

func wsRequestExplicitlyRequestsContinuation(reqBody map[string]json.RawMessage) bool {
	if len(reqBody) == 0 {
		return false
	}
	if raw, ok := reqBody["previous_response_id"]; ok && len(raw) > 0 {
		var previousResponseID string
		if err := json.Unmarshal(raw, &previousResponseID); err == nil && strings.TrimSpace(previousResponseID) != "" {
			return true
		}
	}
	if raw, ok := reqBody["conversation"]; ok && len(raw) > 0 && string(raw) != "null" {
		return true
	}
	return false
}
