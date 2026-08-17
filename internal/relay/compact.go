package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/httpio"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

type responsesCompactRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
}

type responsesCompactResponse struct {
	ID        string                         `json:"id"`
	Object    string                         `json:"object"`
	CreatedAt int64                          `json:"created_at"`
	Output    []openaiOutbound.ResponsesItem `json:"output"`
	Usage     *openaiOutbound.ResponsesUsage `json:"usage,omitempty"`
	Error     *transformerModel.ErrorDetail  `json:"error,omitempty"`
}

// HandleResponsesCompact proxies OpenAI-compatible /responses/compact requests upstream.
func HandleResponsesCompact(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		// middleware.MaxBodySize 命中时要回 413，别把限流当成内部错误。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			resp.ErrorWithCode(c, http.StatusRequestEntityTooLarge, CodeRelayRequestTooLarge, "request body too large")
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	var compactReq responsesCompactRequest
	if err := json.Unmarshal(body, &compactReq); err != nil {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("failed to decode responses compact request: %v", err))
		return
	}
	if strings.TrimSpace(compactReq.Model) == "" {
		resp.Error(c, http.StatusBadRequest, "model is required")
		return
	}
	if len(compactReq.Input) == 0 && compactReq.PreviousResponseID == nil {
		resp.Error(c, http.StatusBadRequest, "either input or previous_response_id is required")
		return
	}

	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, compactReq.Model) {
			resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
			return
		}
	}

	requestModel := compactReq.Model
	apiKeyID := c.GetInt("api_key_id")

	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	metricsReq := &transformerModel.InternalLLMRequest{Model: requestModel, RawRequest: body}
	metrics := NewRelayMetrics(apiKeyID, requestModel, "responses", middleware.ClientIP(c), body, metricsReq)

	var lastErr error
	var capabilityErr error
	var sawSupportedCapability bool
	var lastStatusCode int
	var lastRetryAfter time.Duration
	var lastRetryAt time.Time
	var lastFailure FailureClassification
	var capabilityErrorCode string
	var capabilityErrorMessage string
	capabilityPolicy := getCapabilityDegradationPolicy()

	maxSameChannelRetries := 1
	if group.RetryEnabled {
		maxSameChannelRetries = group.MaxRetries
		if maxSameChannelRetries <= 0 {
			maxSameChannelRetries = 3
		}
	}

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("compact request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		decision := outbound.PlanRelayOperation(channel.Type, outbound.RelayOperationResponsesCompact)
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

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:  make(map[int]struct{}),
			PreferredKeyID: iter.StickyKeyID(),
		}
		var usedKey dbmodel.ChannelKey
		for {
			usedKey = channel.GetChannelKey(selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if !iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				break
			}
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

		var attemptErr error
		var statusCode int
		var retryAfter time.Duration
		var retryAt time.Time
		var failure FailureClassification
		var success bool

		for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
			if retryNum > 0 {
				delay := computeAttemptBackoff(retryNum, retryAt, retryAfter)
				if !waitBackoff(c.Request.Context(), delay) {
					metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
					return
				}
			}

			statusCode, retryAt, attemptErr = forwardResponsesCompactWithRetryAt(
				c,
				metrics,
				iter,
				channel,
				usedKey,
				item.ModelName,
				body,
				capabilityTrace(decision, capabilityPolicy, channel.Type.String()),
			)
			retryAfter = 0
			if !retryAt.IsZero() {
				retryAfter = time.Until(retryAt)
				if retryAfter < 0 {
					retryAfter = 0
				}
			}
			if attemptErr == nil {
				success = true
				break
			}
			failure = classifyRelayFailureContext(c.Request.Context(), statusCode, attemptErr, retryAt)
			if !failure.Retryable {
				break
			}
		}

		usedKey.StatusCode = statusCode
		usedKey.LastUseTimeStamp = time.Now().Unix()
		op.ChannelKeyUpdate(usedKey)

		if success {
			op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestSuccess: 1})
			balancer.RecordSuccess(channel.ID, usedKey.ID, item.ModelName)
			balancer.SetRoutingAffinity(apiKeyID, requestModel, channel.ID, usedKey.ID)
			metrics.SaveWithChannelStats(c.Request.Context(), true, nil, iter.Attempts(), false)
			return
		}

		op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestFailed: 1})
		if failure.Record {
			retryAt = recordFailureAndResolveRetryAt(channel.ID, usedKey.ID, item.ModelName, failure, retryAt)
			failure.RetryAt = retryAt
		}
		iter.InvalidateCurrentPreference()
		lastErr = attemptErr
		lastStatusCode = statusCode
		lastRetryAfter = retryAfter
		lastRetryAt = retryAt
		lastFailure = failure
	}

	finalErr := lastErr
	if !sawSupportedCapability && capabilityErr != nil {
		finalErr = capabilityErr
	}
	metrics.SaveWithChannelStats(c.Request.Context(), false, finalErr, iter.Attempts(), false)
	if !sawSupportedCapability && lastStatusCode == 0 && capabilityErrorCode != "" {
		resp.ErrorWithCode(c, http.StatusBadRequest, capabilityErrorCode, capabilityErrorMessage)
		return
	}
	if lastErr == nil && lastStatusCode == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}
	finalResult := attemptResult{
		Err:        finalErr,
		StatusCode: lastStatusCode,
		RetryAfter: lastRetryAfter,
		RetryAt:    lastRetryAt,
		Failure:    lastFailure,
	}
	if lastFailure.Passthrough || isPassthroughStatus(lastStatusCode) {
		if value := retryAfterHeaderValue(lastRetryAt, time.Now()); value != "" {
			c.Header("Retry-After", value)
		} else if lastRetryAfter > 0 {
			c.Header("Retry-After", retryAfterDurationHeaderValue(lastRetryAfter))
		}
		writeCompactFailure(c, finalResult, finalErr)
		return
	}
	writeCompactFailure(c, finalResult, finalErr)
}

func writeCompactFailure(c *gin.Context, result attemptResult, err error) {
	responseError := protocolErrorForAttempt(result, err)
	if responseError == nil {
		responseError = relayProtocolError(http.StatusBadGateway, CodeRelayUpstreamFailed, "channel failed")
	}
	resp.ErrorWithCode(c, responseError.StatusCode, responseError.Detail.Code, responseError.Detail.Message)
}

func forwardResponsesCompactWithRetryAt(c *gin.Context, metrics *RelayMetrics, iter *balancer.Iterator, channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, mappedModel string, requestBody []byte, trace balancer.CapabilityTrace) (int, time.Time, error) {
	span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
	span.SetCapability(trace)
	requestBody, err := replaceRequiredJSONModel(requestBody, mappedModel)
	if err != nil {
		classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to apply compact model mapping: %w", err))
		span.SetFailure(string(FailureConfiguration), false, time.Time{})
		span.End(dbmodel.AttemptFailed, 0, classified.Error())
		return 0, time.Time{}, classified
	}
	request, err := buildResponsesCompactRequest(c.Request.Context(), channel, usedKey.ChannelKey, requestBody)
	if err != nil {
		classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to create compact request: %w", err))
		span.SetFailure(string(FailureConfiguration), false, time.Time{})
		span.End(dbmodel.AttemptFailed, 0, classified.Error())
		return 0, time.Time{}, classified
	}
	requestBody, captured, err := helper.ApplyParamOverrideWithPayload(request, channel.ParamOverride)
	if err != nil {
		classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("invalid channel param override: %w", err))
		span.SetFailure(string(FailureConfiguration), false, time.Time{})
		span.End(dbmodel.AttemptFailed, 0, classified.Error())
		return 0, time.Time{}, classified
	}
	if !captured {
		requestBody, err = readOutboundRequestBody(request)
		if err != nil {
			classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("failed to inspect compact request: %w", err))
			span.SetFailure(string(FailureConfiguration), false, time.Time{})
			span.End(dbmodel.AttemptFailed, 0, classified.Error())
			return 0, time.Time{}, classified
		}
	}
	actualModel, err := requiredJSONModel(requestBody)
	if err != nil {
		classified := classifyLocalRelayError(FailureConfiguration, fmt.Errorf("channel param override produced an invalid compact request: %w", err))
		span.SetFailure(string(FailureConfiguration), false, time.Time{})
		span.End(dbmodel.AttemptFailed, 0, classified.Error())
		return 0, time.Time{}, classified
	}
	metrics.SetTransportRequestPayload(requestBody, actualModel)
	metrics.ActualModel = actualModel
	copyProxyHeaders(c.Request.Header, channel, request.Header)

	response, err := sendCompactRequest(channel, request)
	if err != nil {
		wrapped := fmt.Errorf("failed to send compact request: %w", err)
		failure := classifyRelayFailureContext(c.Request.Context(), 0, wrapped, time.Time{})
		span.SetFailure(string(failure.Class), failure.Retryable, failure.RetryAt)
		span.End(dbmodel.AttemptFailed, 0, wrapped.Error())
		return 0, time.Time{}, wrapped
	}
	defer response.Body.Close()

	body, readErr := httpio.ReadResponseBody(response.Body)
	if readErr != nil {
		wrapped := fmt.Errorf("failed to read compact response body: %w", readErr)
		failure := classifyRelayFailureContext(c.Request.Context(), responseProcessingErrorStatus(response.StatusCode), wrapped, time.Time{})
		span.SetFailure(string(failure.Class), failure.Retryable, failure.RetryAt)
		span.End(dbmodel.AttemptFailed, response.StatusCode, wrapped.Error())
		return responseProcessingErrorStatus(response.StatusCode), time.Time{}, wrapped
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryAt := parseRetryAt(response.Header.Get("Retry-After"))
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		responseErr := transformerModel.NormalizeHTTPError(statusCode, response.Header, body, "api_error")
		failure := classifyRelayFailureContext(c.Request.Context(), statusCode, responseErr, retryAt)
		span.SetFailure(string(failure.Class), failure.Retryable, retryAt)
		span.End(dbmodel.AttemptFailed, statusCode, responseErr.Error())
		return statusCode, retryAt, responseErr
	}

	copyProxyResponseHeaders(c.Writer.Header(), response.Header)
	contentType := response.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	c.Data(response.StatusCode, contentType, body)

	var compactResp responsesCompactResponse
	if err := json.Unmarshal(body, &compactResp); err == nil {
		metrics.SetInternalResponse(compactResponseToInternalResponse(&compactResp), actualModel)
	}

	span.End(dbmodel.AttemptSuccess, response.StatusCode, "")
	return response.StatusCode, time.Time{}, nil
}

func buildResponsesCompactRequest(ctx context.Context, channel *dbmodel.Channel, key string, requestBody []byte) (*http.Request, error) {
	parsedURL, err := url.Parse(strings.TrimSuffix(channel.GetBaseUrl(), "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + "/responses/compact"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

func copyProxyHeaders(src http.Header, channel *dbmodel.Channel, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		if hopByHopHeaders[lowerKey] || lowerKey == "content-type" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	for _, header := range channel.CustomHeader {
		if strings.EqualFold(header.HeaderKey, "Content-Type") {
			continue
		}
		dst.Set(header.HeaderKey, header.HeaderValue)
	}
	// 防止 Go 默认 User-Agent 泄露到上游
	if dst.Get("User-Agent") == "" {
		dst.Set("User-Agent", "")
	}
}

func copyProxyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func sendCompactRequest(channel *dbmodel.Channel, req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), channel)
	if err != nil {
		return nil, classifyLocalRelayError(FailureConfiguration, err)
	}
	return httpClient.Do(req)
}

func compactResponseToInternalResponse(resp *responsesCompactResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}
	return &transformerModel.InternalLLMResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: resp.CreatedAt,
		Usage:   convertCompactUsage(resp.Usage),
	}
}

func convertCompactUsage(usage *openaiOutbound.ResponsesUsage) *transformerModel.Usage {
	if usage == nil {
		return nil
	}
	result := &transformerModel.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.InputTokenDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &transformerModel.PromptTokensDetails{
			CachedTokens: usage.InputTokenDetails.CachedTokens,
		}
	}
	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &transformerModel.CompletionTokensDetails{
			ReasoningTokens: usage.OutputTokenDetails.ReasoningTokens,
		}
	}
	return result
}
