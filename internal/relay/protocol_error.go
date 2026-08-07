package relay

import (
	"context"
	"net/http"

	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

func relayProtocolError(status int, code, message string) *model.ResponseError {
	errorType := "api_error"
	if status >= 400 && status < 500 {
		errorType = "invalid_request_error"
	}
	return &model.ResponseError{
		StatusCode: status,
		Detail: model.ErrorDetail{
			Code:    code,
			Message: message,
			Type:    errorType,
		},
	}
}

func protocolErrorFromError(status int, err error) *model.ResponseError {
	if responseError, ok := err.(*model.ResponseError); ok {
		return model.NormalizeResponseError(responseError, status, "api_error")
	}
	message := http.StatusText(status)
	if err != nil && err.Error() != "" {
		message = err.Error()
	}
	return relayProtocolError(status, CodeRelayUpstreamFailed, message)
}

func writeInboundProtocolError(c *gin.Context, heartbeat *earlyHeartbeat, inbound model.Inbound, responseError *model.ResponseError) {
	if c == nil || inbound == nil {
		return
	}
	normalized := model.NormalizeResponseError(responseError, http.StatusBadGateway, "api_error")
	transformed, err := inbound.TransformError(c.Request.Context(), normalized)
	if err != nil || transformed == nil || len(transformed.Body) == 0 {
		resp.ErrorWithCode(c, normalized.StatusCode, normalized.Detail.Code, normalized.Detail.Message)
		return
	}

	if heartbeat != nil && heartbeat.HeaderWritten() {
		body := transformed.StreamBody
		if len(body) == 0 {
			body = transformed.Body
		}
		heartbeat.mu.Lock()
		defer heartbeat.mu.Unlock()
		_, _ = c.Writer.Write(body)
		c.Writer.Flush()
		return
	}

	for key, values := range transformed.Headers {
		for _, value := range values {
			c.Header(key, value)
		}
	}
	c.Data(transformed.StatusCode, transformed.Headers.Get("Content-Type"), transformed.Body)
}

func writeStreamProtocolError(ctx context.Context, writer StreamWriter, inbound model.Inbound, responseError *model.ResponseError) {
	if writer == nil || inbound == nil || responseError == nil {
		return
	}
	transformed, err := inbound.TransformError(ctx, responseError)
	if err != nil || transformed == nil || len(transformed.StreamBody) == 0 {
		return
	}
	_, _ = writer.Write(transformed.StreamBody)
	writer.Flush()
}
