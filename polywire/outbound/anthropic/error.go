package anthropic

import (
	"context"
	"net/http"

	"github.com/bestruirui/octopus/polywire/model"
)

func (o *MessageOutbound) TransformError(_ context.Context, statusCode int, headers http.Header, body []byte) *model.ResponseError {
	return model.NormalizeHTTPError(statusCode, headers, body, "api_error")
}
