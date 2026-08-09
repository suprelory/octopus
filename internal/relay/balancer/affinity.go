package balancer

import (
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

const defaultChannelAffinityTTL = time.Hour

// channelAffinity stores the channel/key that completed the latest real model
// request. It is process-local and independent of the legacy group sticky TTL.
var channelAffinity sync.Map // key: apiKeyID:requestModel -> *SessionEntry

func channelAffinityEnabled() bool {
	enabled, err := op.SettingGetBool(model.SettingKeyChannelAffinityEnabled)
	if err != nil {
		return true
	}
	return enabled
}

func channelAffinityTTL() time.Duration {
	seconds, err := op.SettingGetInt(model.SettingKeyChannelAffinityTTLSeconds)
	if err != nil || seconds < 1 {
		return defaultChannelAffinityTTL
	}
	ttl := time.Duration(seconds) * time.Second
	if ttl <= 0 {
		return defaultChannelAffinityTTL
	}
	return ttl
}

// GetChannelAffinity reads an unexpired affinity while the switch is enabled.
func GetChannelAffinity(apiKeyID int, requestModel string) *SessionEntry {
	if !channelAffinityEnabled() {
		return nil
	}
	key := sessionKey(apiKeyID, requestModel)
	value, ok := channelAffinity.Load(key)
	if !ok {
		return nil
	}
	entry, ok := value.(*SessionEntry)
	if !ok || entry == nil {
		channelAffinity.Delete(key)
		return nil
	}
	if time.Since(entry.Timestamp) > channelAffinityTTL() {
		channelAffinity.Delete(key)
		return nil
	}
	cloned := *entry
	return &cloned
}

// SetChannelAffinity records a fully successful real request. Disabled affinity
// neither writes nor refreshes entries, and existing entries remain in memory.
func SetChannelAffinity(apiKeyID int, requestModel string, channelID, keyID int) {
	if !channelAffinityEnabled() || channelID <= 0 {
		return
	}
	channelAffinity.Store(sessionKey(apiKeyID, requestModel), &SessionEntry{
		ChannelID:    channelID,
		ChannelKeyID: keyID,
		Timestamp:    time.Now(),
	})
}

func DeleteChannelAffinity(apiKeyID int, requestModel string) {
	channelAffinity.Delete(sessionKey(apiKeyID, requestModel))
}

// SetRoutingAffinity refreshes both system affinity and legacy group sticky.
func SetRoutingAffinity(apiKeyID int, requestModel string, channelID, keyID int) {
	SetChannelAffinity(apiKeyID, requestModel, channelID, keyID)
	SetSticky(apiKeyID, requestModel, channelID, keyID)
}

// DeleteRoutingAffinity does not touch Responses replay/previous_response_id.
func DeleteRoutingAffinity(apiKeyID int, requestModel string) {
	DeleteChannelAffinity(apiKeyID, requestModel)
	DeleteSticky(apiKeyID, requestModel)
}

func resetChannelAffinityByChannel(channelID int) {
	channelAffinity.Range(func(key, value any) bool {
		entry, ok := value.(*SessionEntry)
		if ok && entry != nil && entry.ChannelID == channelID {
			channelAffinity.Delete(key)
		}
		return true
	})
}
