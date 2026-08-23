package balancer

import (
	"fmt"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

const defaultChannelAffinityTTL = time.Hour

type SessionEntry struct {
	GroupID      int
	ChannelID    int
	ChannelKeyID int
	Timestamp    time.Time
}

func sessionKey(apiKeyID, groupID int, requestModel string) string {
	return fmt.Sprintf("%d:%d:%s", apiKeyID, groupID, requestModel)
}

// channelAffinity stores the channel/key that completed the latest real model
// request. The scope is API key + group + request model. It is process-local
// and uses the global affinity TTL.
var channelAffinity sync.Map // key: apiKeyID:groupID:requestModel -> *SessionEntry

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
func GetChannelAffinity(apiKeyID, groupID int, requestModel string) *SessionEntry {
	if !channelAffinityEnabled() {
		return nil
	}
	key := sessionKey(apiKeyID, groupID, requestModel)
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
func SetChannelAffinity(apiKeyID, groupID int, requestModel string, channelID, keyID int) {
	if !channelAffinityEnabled() || channelID <= 0 {
		return
	}
	channelAffinity.Store(sessionKey(apiKeyID, groupID, requestModel), &SessionEntry{
		GroupID:      groupID,
		ChannelID:    channelID,
		ChannelKeyID: keyID,
		Timestamp:    time.Now(),
	})
}

func DeleteChannelAffinity(apiKeyID, groupID int, requestModel string) {
	channelAffinity.Delete(sessionKey(apiKeyID, groupID, requestModel))
}

// SetRoutingAffinity records the latest fully successful route.
func SetRoutingAffinity(apiKeyID, groupID int, requestModel string, channelID, keyID int) {
	SetChannelAffinity(apiKeyID, groupID, requestModel, channelID, keyID)
}

// DeleteRoutingAffinity does not touch Responses replay/previous_response_id.
func DeleteRoutingAffinity(apiKeyID, groupID int, requestModel string) {
	DeleteChannelAffinity(apiKeyID, groupID, requestModel)
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

func resetChannelAffinityByGroup(groupID int) {
	channelAffinity.Range(func(key, value any) bool {
		entry, ok := value.(*SessionEntry)
		if ok && entry != nil && entry.GroupID == groupID {
			channelAffinity.Delete(key)
		}
		return true
	})
}
