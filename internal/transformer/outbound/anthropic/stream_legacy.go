package anthropic

import (
	"context"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TransformStream retains the chunk interface using the canonical event parser.
func (o *MessageOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	events, err := o.TransformStreamEvent(ctx, eventData)
	if err != nil {
		return nil, err
	}
	return model.InternalResponseFromStreamEvents(events), nil
}
