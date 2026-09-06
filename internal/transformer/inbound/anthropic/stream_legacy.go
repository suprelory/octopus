package anthropic

import (
	"context"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TransformStream retains the chunk interface using the canonical event encoder.
func (i *MessagesInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
	return i.TransformStreamEvents(ctx, model.StreamEventsFromInternalResponse(stream))
}
