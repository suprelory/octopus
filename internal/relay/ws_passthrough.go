package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/stream"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

type wsPassthroughStats struct {
	ResponseID string
	Model      string
	Usage      *transformerModel.Usage
	RawOutput  json.RawMessage
	Error      *wsUpstreamEventError
	Stream     transformerModel.StreamDiagnostics
}

type wsUpstreamEventError = openaiOutbound.StreamUpstreamError

func (ra *relayAttempt) forwardViaWSPassthrough(ctx context.Context) (int, error) {
	continuation := requiresUpstreamWSContinuation(ra.internalRequest)
	preferredConnID := ""
	redial := ra.transportRecovery == upstreamRecoveryReconnect
	if continuation && !redial {
		preferredConnID, _ = getWSResponseConn(currentPreviousResponseID(ra.internalRequest))
	}
	pc := TryUpstreamWSWithPreference(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), preferredConnID, redial)
	if pc == nil {
		log.Debugf("upstream WS passthrough unavailable for channel %s (key=%d, continuation=%t)", ra.channel.Name, ra.usedKey.ID, continuation)
		return -1, nil
	}

	payload, err := ra.buildWSPassthroughRequestPayload()
	if err != nil {
		wsUpstreamPool.Put(pc)
		return 0, classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to build websocket passthrough request: %w", err))
	}
	ra.metrics.SetTransportRequestPayload(payload, ra.internalRequest.Model)
	if err := ra.reserveSubmission(ctx); err != nil {
		wsUpstreamPool.Put(pc)
		return 0, err
	}
	if redial {
		ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReconnect)
	}
	ra.upstreamTransport = "ws"
	if err := wsUpstreamPool.SendRaw(ctx, pc, payload); err != nil {
		log.Warnf("upstream WS passthrough send failed for channel %s: %v", ra.channel.Name, err)
		wsUpstreamPool.RemoveConn(pc)
		return ra.upstreamWSFailure(ctx, 0, err, true)
	}

	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModePassthrough)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	stats, err := ra.handleWSPassthroughStream(ctx, pc)
	if err != nil {
		if ra.responseCommitted() {
			ra.applyWSPassthroughStats(stats)
		}
		if stats != nil && stats.Error != nil {
			ra.captureRetryAt(stats.Error.RetryAt)
		}
		if isUpstreamWSRequestError(err) {
			wsUpstreamPool.Put(pc)
		} else {
			wsUpstreamPool.RemoveConn(pc)
		}
		statusCode := http.StatusBadGateway
		if stats != nil && stats.Error != nil && stats.Error.Status > 0 {
			statusCode = stats.Error.Status
		}
		return ra.upstreamWSFailure(ctx, statusCode, err, false)
	}
	wsUpstreamPool.Put(pc)
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.applyWSPassthroughStats(stats)
	ra.recordSuccessfulWSAffinity(pc)
	return http.StatusOK, nil
}

func (ra *relayAttempt) buildWSPassthroughRequestPayload() ([]byte, error) {
	body := ra.rawBody
	if len(body) == 0 {
		responsesReq := openaiOutbound.ConvertToResponsesRequest(ra.internalRequest)
		var err error
		body, err = transformerModel.MarshalRequestWithRecovery(ra.internalRequest, transformerModel.APIFormatOpenAIResponse, responsesReq)
		if err != nil {
			return nil, err
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["type"] = json.RawMessage(`"response.create"`)
	payload["stream"] = json.RawMessage(`true`)
	delete(payload, "background")
	if ra.internalRequest != nil && strings.TrimSpace(ra.internalRequest.Model) != "" {
		modelBytes, err := json.Marshal(ra.internalRequest.Model)
		if err != nil {
			return nil, err
		}
		payload["model"] = modelBytes
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if channelParamOverrideConfigured(ra.channel) {
		encoded, err = ra.applyParamOverridePayload(encoded)
		if err != nil {
			return nil, err
		}
	}
	if err := validateWSResponseCreatePayload(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (ra *relayAttempt) handleWSPassthroughStream(ctx context.Context, pc *pooledConn) (_ *wsPassthroughStats, resultErr error) {
	writer := ra.getStreamWriter()
	stats := &wsPassthroughStats{}
	converter := ra.ensureStreamConverter()
	stats.Stream.SourceTransport = transformerModel.SourceTransportWebSocket
	defer func() {
		stats.Stream.CompletionStatus = "completed"
		stats.Stream.FinishCause = transformerModel.StreamFinishCauseExplicitTerminal
		if resultErr != nil {
			stats.Stream.CompletionStatus = "interrupted"
			stats.Stream.FinishCause = transformerModel.StreamFinishCauseSourceError
			if stats.Stream.CleanEOF {
				stats.Stream.FinishCause = transformerModel.StreamFinishCauseCleanEOF
			}
			if isClientCancellation(ctx, resultErr) {
				stats.Stream.CompletionStatus = "canceled"
				stats.Stream.FinishCause = transformerModel.StreamFinishCauseClientCancellation
			}
		}
		if !converter.Completed() {
			_, finishErr := converter.Finish(ctx, stats.Stream.FinishCause)
			if resultErr == nil && finishErr != nil {
				resultErr = finishErr
				stats.Stream.CompletionStatus = "interrupted"
			}
		}
		diagnostics := stats.Stream
		ra.streamDiagnostics = &diagnostics
	}()
	firstEvent := true
	dropDownstream := false
	readCtx := ctx
	for {
		msgType, data, err := pc.conn.Read(readCtx)
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
				stats.Stream.CleanEOF = true
				if firstEvent {
					return stats, fmt.Errorf("ws stream ended before first event")
				}
				return stats, transformerModel.ErrStreamIncomplete
			}
			return stats, fmt.Errorf("ws passthrough read error: %w", err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		observeWSPassthroughEvent(stats, data)
		if stats.Error != nil {
			if !dropDownstream && ra.responseCommitted() {
				out := ra.rewriteWSPassthroughDownstreamModel(data)
				ra.protocolErrorWritten = true
				if writeErr := writeWSPassthroughDownstream(ctx, writer, out); writeErr != nil {
					log.Debugf("ws passthrough: failed to forward upstream error frame downstream (channel=%d, key=%d): %v", ra.channel.ID, ra.usedKey.ID, writeErr)
				}
			}
			return stats, stats.Error
		}
		if _, err := converter.Push(readCtx, transformerModel.SourceEvent{Type: stats.Stream.LastEventType, Data: data, Sequence: stats.Stream.LastSourceSequence, Transport: transformerModel.SourceTransportWebSocket}); err != nil {
			return stats, err
		}
		if !dropDownstream {
			out := ra.rewriteWSPassthroughDownstreamModel(data)
			ra.commitResponse()
			if writeErr := writeWSPassthroughDownstream(ctx, writer, out); writeErr != nil {
				if isClientCancellation(ctx, writeErr) || isUpstreamWSConnectionBroken(writeErr) {
					log.Debugf("ws passthrough downstream write failed; draining upstream (channel=%d, key=%d): %v", ra.channel.ID, ra.usedKey.ID, writeErr)
					dropDownstream = true
					if readCtx == ctx && isClientCancellation(ctx, writeErr) {
						drainCtx, drainCancel := context.WithTimeout(context.Background(), wsPassthroughDrainTimeout)
						defer drainCancel()
						readCtx = drainCtx
					}
					continue
				}
				return stats, fmt.Errorf("%w: %w", stream.ErrDownstreamWrite, writeErr)
			}
			if firstEvent {
				if ra.metrics != nil {
					ra.metrics.SetFirstTokenTime(time.Now())
				}
				ra.stopFirstTokenTimer()
			}
		}
		firstEvent = false
		if stats.Stream.TerminalEventSeen {
			return stats, nil
		}
	}
}

func writeWSPassthroughDownstream(ctx context.Context, writer StreamWriter, out []byte) error {
	if wsWriter, ok := writer.(*WSStreamWriter); ok {
		return wsWriter.writeFrame(ctx, out)
	}
	if _, writeErr := writer.Write([]byte("data: " + string(out) + "\n\n")); writeErr != nil {
		return writeErr
	}
	writer.Flush()
	return nil
}

func (ra *relayAttempt) rewriteWSPassthroughDownstreamModel(data []byte) []byte {
	if ra == nil || ra.internalRequest == nil || strings.TrimSpace(ra.requestModel) == "" || strings.TrimSpace(ra.internalRequest.Model) == strings.TrimSpace(ra.requestModel) {
		return data
	}
	return openaiOutbound.RewriteResponseEventModel(data, ra.internalRequest.Model, ra.requestModel)
}

func observeWSPassthroughEvent(stats *wsPassthroughStats, data []byte) {
	if stats == nil || len(data) == 0 {
		return
	}
	stats.Stream.EventsReceived++
	stats.Stream.LastSourceSequence = stats.Stream.EventsReceived
	stats.Stream.BytesReceived += int64(len(data))
	observation, err := openaiOutbound.InspectResponseEvent(data, time.Now())
	if err != nil {
		stats.Error = &wsUpstreamEventError{Status: 502, Message: "invalid Responses stream event: " + err.Error()}
		return
	}
	stats.Stream.LastEventType = observation.Type
	stats.Stream.TerminalEventSeen = stats.Stream.TerminalEventSeen || observation.Terminal
	stats.Stream.FinishReasonSeen = stats.Stream.FinishReasonSeen || observation.FinishReasonSeen
	if observation.ResponseID != "" {
		stats.ResponseID = observation.ResponseID
	}
	if observation.Model != "" {
		stats.Model = observation.Model
	}
	if observation.Usage != nil {
		stats.Usage = observation.Usage
	}
	if len(observation.RawOutput) > 0 {
		stats.RawOutput = observation.RawOutput
	}
	if observation.Error != nil {
		stats.Error = observation.Error
	}
}

func normalizeWSUpstreamErrorCode(code any) string {
	return openaiOutbound.NormalizeStreamErrorCode(code)
}

func isWSPassthroughTerminal(data []byte) bool {
	observation, err := openaiOutbound.InspectResponseEvent(data, time.Now())
	return err == nil && observation.Terminal
}

func (ra *relayAttempt) applyWSPassthroughStats(stats *wsPassthroughStats) {
	if ra == nil || ra.metrics == nil || stats == nil {
		return
	}
	if ra.streamConverter != nil {
		if response := ra.streamConverter.Response(); response != nil {
			modelName := response.Model
			if modelName == "" && ra.internalRequest != nil {
				modelName = ra.internalRequest.Model
			}
			ra.metrics.SetInternalResponse(response, modelName)
			ra.responseCollected.Store(true)
		}
		return
	}
	modelName := strings.TrimSpace(stats.Model)
	if modelName == "" && ra.internalRequest != nil {
		modelName = ra.internalRequest.Model
	}
	resp := &transformerModel.InternalLLMResponse{
		ID:                      stats.ResponseID,
		Object:                  "response",
		Created:                 time.Now().Unix(),
		Model:                   modelName,
		Usage:                   stats.Usage,
		RawResponsesOutputItems: stats.RawOutput,
	}
	ra.metrics.SetInternalResponse(resp, modelName)
}
