package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func (ra *relayAttempt) handleWSStreamResponseV2(ctx context.Context, reader *wsUpstreamReader) error {
	defer ra.closeFirstTokenBudget()
	return ra.handleTransformedStream(ctx, stream.NewWSSource(reader), nil)
}

// handleTransformedStream shares semantic precommit and finalization across
// HTTP SSE and upstream WebSocket events.
func (ra *relayAttempt) handleTransformedStream(ctx context.Context, source stream.StreamSource, timeoutCloser io.Closer) error {
	ra.heartbeat.Hand()

	semanticPayload := false
	transform := func(ctx context.Context, data []byte) ([]byte, error) {
		var err error
		var output []byte
		output, semanticPayload, err = ra.transformStreamData(ctx, string(data))
		return output, err
	}
	precommit := func(_, _ []byte) bool { return semanticPayload }

	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:             source,
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
	return ra.runStreamProcessor(ctx, processor, timeoutCloser)
}

// runStreamProcessor preserves the distinction between response headers,
// heartbeat bytes and a committed semantic payload when deciding retry safety.
func (ra *relayAttempt) runStreamProcessor(ctx context.Context, processor *stream.StreamProcessor, timeoutCloser io.Closer) error {
	err := processor.Run()
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		if timeoutCloser != nil {
			_ = timeoutCloser.Close()
		}
		return ra.firstTokenTimeoutError()
	}
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}
	return err
}

// getStreamWriter returns the appropriate stream writer for the current request.
func (ra *relayAttempt) getStreamWriter() StreamWriter {
	if ra.streamWriter != nil {
		return ra.streamWriter
	}
	return ra.c.Writer
}

// handleStreamResponseV2 validates the HTTP envelope before processing events.
func (ra *relayAttempt) handleStreamResponseV2(ctx context.Context, response *http.Response) error {
	defer ra.closeFirstTokenBudget()
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}
	return ra.handleTransformedStream(ctx, stream.NewSSESource(response.Body, maxSSEEventSize), response.Body)
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

	return ra.runStreamProcessor(ctx, processor, response.Body)
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
