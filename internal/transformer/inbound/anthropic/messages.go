package anthropic

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type MessagesInbound struct {
	// Stream state tracking
	hasStarted                bool
	hasTextContentStarted     bool
	hasThinkingContentStarted bool
	hasToolContentStarted     bool
	hasFinished               bool
	messageStopped            bool
	messageID                 string
	modelName                 string
	requestModel              string
	contentIndex              int64
	stopReason                *string
	stopSequence              *string
	pendingUsage              *model.Usage
	toolCallIndices           map[int]bool // Track which tool call indices we've seen
	inputToken                int64

	streamAggregator model.StreamAggregator
	// storedResponse stores the non-stream response
	storedResponse *model.InternalLLMResponse
}

// SeedRequestState re-initializes the request-derived state that
// TransformRequest would have produced, so a retry attempt can reuse the
// already-parsed request instead of re-parsing and re-counting the body.
func (i *MessagesInbound) SeedRequestState(request *model.InternalLLMRequest) {
	if i == nil || request == nil {
		return
	}
	i.requestModel = strings.TrimSpace(request.Model)
	i.inputToken = request.EstimatedInputTokens
}

// GetInternalResponse returns the complete internal response for logging, statistics, etc.
// For streaming: aggregates all stored stream chunks into a complete response
// For non-streaming: returns the stored response
func (i *MessagesInbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	if i.storedResponse != nil {
		return i.storedResponse, nil
	}
	return i.streamAggregator.BuildAndReset(), nil
}
