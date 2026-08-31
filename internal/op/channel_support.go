package op

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// validateChannelReference verifies that a channel can still participate in
// routing. Historical rows for removed providers remain in the database for
// audit purposes, but they must not be introduced into new group bindings.
func validateChannelReference(channelID int) (model.Channel, error) {
	if channelID <= 0 {
		return model.Channel{}, fmt.Errorf("channel id is required")
	}
	channel, ok := channelCache.Get(channelID)
	if !ok {
		return model.Channel{}, fmt.Errorf("channel not found")
	}
	if _, ok := outbound.Descriptor(channel.Type); !ok {
		return model.Channel{}, fmt.Errorf("unsupported channel type: %d", channel.Type)
	}
	return channel, nil
}

func supportedChannelType(channelType outbound.OutboundType) bool {
	_, ok := outbound.Descriptor(channelType)
	return ok
}
