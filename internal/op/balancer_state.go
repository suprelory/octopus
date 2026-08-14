package op

var (
	resetRelayBalancerStateForChannel func(int)
	resetRelayBalancerStateForGroup   func(int)
)

func RegisterRelayBalancerStateReset(fn func(int)) {
	resetRelayBalancerStateForChannel = fn
}

func RegisterRelayBalancerGroupStateReset(fn func(int)) {
	resetRelayBalancerStateForGroup = fn
}

func resetBalancerStateForChannel(channelID int) {
	if resetRelayBalancerStateForChannel != nil {
		resetRelayBalancerStateForChannel(channelID)
	}
}

func resetBalancerStateForChannels(channelIDs ...int) {
	if resetRelayBalancerStateForChannel == nil || len(channelIDs) == 0 {
		return
	}
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID == 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		resetRelayBalancerStateForChannel(channelID)
	}
}

func resetBalancerStateForGroup(groupID int) {
	if resetRelayBalancerStateForGroup != nil && groupID != 0 {
		resetRelayBalancerStateForGroup(groupID)
	}
}

func resetBalancerStateForGroups(groupIDs ...int) {
	if resetRelayBalancerStateForGroup == nil || len(groupIDs) == 0 {
		return
	}
	seen := make(map[int]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID == 0 {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		resetRelayBalancerStateForGroup(groupID)
	}
}
