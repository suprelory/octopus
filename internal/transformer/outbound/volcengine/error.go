package volcengine

import (
	"context"
	"net/http"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func (o *ResponseOutbound) TransformError(ctx context.Context, statusCode int, headers http.Header, body []byte) *model.ResponseError {
	return o.inner.TransformError(ctx, statusCode, headers, body)
}
