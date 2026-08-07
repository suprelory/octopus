package gemini

import (
	"context"
	"net/http"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func (o *MessagesOutbound) TransformError(_ context.Context, statusCode int, headers http.Header, body []byte) *model.ResponseError {
	return model.NormalizeHTTPError(statusCode, headers, body, "upstream_error")
}
