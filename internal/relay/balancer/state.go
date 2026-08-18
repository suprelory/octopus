package balancer

import "github.com/bestruirui/octopus/internal/op"

func init() {
	op.RegisterRelayBalancerStateReset(ResetStateByChannel)
	op.RegisterRelayBalancerGroupStateReset(ResetStateByGroup)
}

func ResetStateByChannel(channelID int) {
	resetCircuitBreakerByChannel(channelID)
	resetChannelAffinityByChannel(channelID)
	globalStrategyState.resetByChannel(channelID)
	globalChannelHealth.resetByChannel(channelID)
}

func ResetStateByGroup(groupID int) {
	globalStrategyState.resetByGroup(groupID)
}
