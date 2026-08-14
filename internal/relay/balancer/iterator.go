package balancer

import (
	"fmt"
	"sort"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// Iterator 统一的负载均衡迭代器
// 内部编排：策略排序 + 粘性优先 + 决策追踪
type Iterator struct {
	candidates   []model.GroupItem
	tiers        []candidateTier
	index        int
	preferences  map[int]routingPreference
	apiKeyID     int
	requestModel string
	modelName    string // 请求模型名（用于熔断检查）

	// 内嵌追踪
	attempts []model.ChannelAttempt
	count    int
}

// QualityRanker returns a lower-is-better semantic conversion quality rank for
// a candidate. The iterator partitions candidates by rank, then applies the
// configured load-balancer independently inside each quality tier.
type QualityRanker func(model.GroupItem) int

// PreferenceSource identifies why a candidate was moved ahead of the normal
// load-balancer order. The numeric order also represents routing priority.
type PreferenceSource uint8

const (
	PreferenceNone PreferenceSource = iota
	PreferenceResponsesReplay
	PreferenceChannelAffinity
	PreferenceLegacySticky
)

type routingPreference struct {
	source PreferenceSource
	entry  SessionEntry
}

type candidateTier struct {
	start     int
	end       int
	mode      model.GroupMode
	scope     balanceScope
	scheduled bool
}

type CapabilityTrace struct {
	AdapterType      string
	Status           string
	Policy           string
	ConversionPath   []string
	RequiredFeatures []string
	DegradedFields   []string
	Losses           []model.CapabilityLoss
	Lossiness        string
	Reasons          []string
}

// NewIterator 创建负载均衡迭代器
// 自动处理：策略排序 + 粘性通道提前
func NewIterator(group model.Group, apiKeyID int, requestModel string) *Iterator {
	return NewIteratorWithPreferenceAndQuality(group, apiKeyID, requestModel, nil, nil)
}

// NewIteratorWithPreference 创建带优先通道偏好的负载均衡迭代器。
// preferred 非空时，会优先把指定通道提前到候选列表最前面。
func NewIteratorWithPreference(group model.Group, apiKeyID int, requestModel string, preferred *SessionEntry) *Iterator {
	return NewIteratorWithPreferenceAndQuality(group, apiKeyID, requestModel, preferred, nil)
}

// NewIteratorWithPreferenceAndQuality is the request-aware iterator constructor.
// Quality is evaluated before sticky preferences are applied; a sticky route
// therefore remains preferred only among candidates in the same quality tier.
func NewIteratorWithPreferenceAndQuality(group model.Group, apiKeyID int, requestModel string, preferred *SessionEntry, quality QualityRanker) *Iterator {
	rankedTiers := partitionQualityTiers(group.Items, quality)
	preferenceCandidates := make([]routingPreference, 0, 3)
	if preferred != nil && preferred.ChannelID > 0 {
		preferenceCandidates = append(preferenceCandidates, routingPreference{
			source: PreferenceResponsesReplay,
			entry:  *preferred,
		})
	}
	if affinity := GetChannelAffinity(apiKeyID, requestModel); affinity != nil {
		preferenceCandidates = append(preferenceCandidates, routingPreference{
			source: PreferenceChannelAffinity,
			entry:  *affinity,
		})
	}
	if group.SessionKeepTime > 0 {
		stickyTTL := time.Duration(group.SessionKeepTime) * time.Second
		if sticky := GetSticky(apiKeyID, requestModel, stickyTTL); sticky != nil {
			preferenceCandidates = append(preferenceCandidates, routingPreference{
				source: PreferenceLegacySticky,
				entry:  *sticky,
			})
		}
	}

	candidates := make([]model.GroupItem, 0, len(group.Items))
	preferences := make(map[int]routingPreference, len(preferenceCandidates))
	selectedChannels := make(map[int]struct{}, len(preferenceCandidates))
	for _, preference := range preferenceCandidates {
		channelID := preference.entry.ChannelID
		if channelID <= 0 {
			continue
		}
		if _, selected := selectedChannels[channelID]; selected {
			continue
		}

		preferredItem, preferredTier, found := findPreferredCandidate(rankedTiers, channelID)
		if !found {
			switch preference.source {
			case PreferenceChannelAffinity:
				DeleteChannelAffinity(apiKeyID, requestModel)
			case PreferenceLegacySticky:
				DeleteSticky(apiKeyID, requestModel)
			}
			continue
		}
		if preferredTier != 0 {
			continue
		}

		preferences[len(candidates)] = preference
		candidates = append(candidates, preferredItem)
		selectedChannels[channelID] = struct{}{}

		// A group may contain multiple mappings for the same channel. Once a
		// preferred channel is selected, remove its duplicate fallback items.
		for tierIndex := range rankedTiers {
			filtered := rankedTiers[tierIndex].items[:0]
			for _, item := range rankedTiers[tierIndex].items {
				if item.ChannelID != channelID {
					filtered = append(filtered, item)
				}
			}
			rankedTiers[tierIndex].items = filtered
		}
	}

	tiers := make([]candidateTier, 0, len(rankedTiers))
	for _, rankedTier := range rankedTiers {
		if len(rankedTier.items) == 0 {
			continue
		}
		start := len(candidates)
		candidates = append(candidates, rankedTier.items...)
		tiers = append(tiers, candidateTier{
			start: start,
			end:   len(candidates),
			mode:  group.Mode,
			scope: balanceScope{
				groupID:       group.ID,
				requestModel:  requestModel,
				qualityTier:   rankedTier.rank,
				qualityRanked: quality != nil,
			},
		})
	}

	return &Iterator{
		candidates:   candidates,
		tiers:        tiers,
		index:        -1,
		preferences:  preferences,
		apiKeyID:     apiKeyID,
		requestModel: requestModel,
		modelName:    requestModel,
	}
}

type rankedCandidateTier struct {
	rank  int
	items []model.GroupItem
}

func partitionQualityTiers(items []model.GroupItem, quality QualityRanker) []rankedCandidateTier {
	if len(items) == 0 {
		return nil
	}
	if quality == nil {
		return []rankedCandidateTier{{items: canonicalCandidates(items)}}
	}

	byRank := make(map[int][]model.GroupItem)
	ranks := make([]int, 0)
	for _, item := range items {
		rank := quality(item)
		if _, exists := byRank[rank]; !exists {
			ranks = append(ranks, rank)
		}
		byRank[rank] = append(byRank[rank], item)
	}
	sort.Ints(ranks)
	result := make([]rankedCandidateTier, 0, len(ranks))
	for _, rank := range ranks {
		result = append(result, rankedCandidateTier{rank: rank, items: canonicalCandidates(byRank[rank])})
	}
	return result
}

func findPreferredCandidate(tiers []rankedCandidateTier, channelID int) (model.GroupItem, int, bool) {
	for tierIndex, tier := range tiers {
		for _, item := range tier.items {
			if item.ChannelID == channelID {
				return item, tierIndex, true
			}
		}
	}
	return model.GroupItem{}, -1, false
}

// Next 移动到下一个候选，返回 false 表示遍历完成
func (it *Iterator) Next() bool {
	next := it.index + 1
	if next >= len(it.candidates) {
		it.index = next
		return false
	}
	for tierIndex := range it.tiers {
		tier := &it.tiers[tierIndex]
		if tier.scheduled || tier.start != next {
			continue
		}
		ordered := getBalancer(tier.mode, tier.scope).Candidates(it.candidates[tier.start:tier.end])
		copy(it.candidates[tier.start:tier.end], ordered)
		tier.scheduled = true
		break
	}
	it.index = next
	return true
}

// Item 返回当前候选的 GroupItem
func (it *Iterator) Item() model.GroupItem {
	return it.candidates[it.index]
}

// IsSticky 当前候选是否为粘性通道
func (it *Iterator) IsSticky() bool {
	_, ok := it.preferences[it.index]
	return ok
}

func (it *Iterator) StickyKeyID() int {
	preference, ok := it.preferences[it.index]
	if !ok {
		return 0
	}
	return preference.entry.ChannelKeyID
}

func (it *Iterator) PreferenceSource() PreferenceSource {
	preference, ok := it.preferences[it.index]
	if !ok {
		return PreferenceNone
	}
	return preference.source
}

// InvalidateCurrentPreference clears ordinary affinity after the preferred
// channel proves unusable before downstream payload is written. Replay state
// and replay routing preferences remain independent and untouched.
func (it *Iterator) InvalidateCurrentPreference() {
	preference, ok := it.preferences[it.index]
	if !ok || preference.source == PreferenceResponsesReplay {
		return
	}
	DeleteRoutingAffinity(it.apiKeyID, it.requestModel)
	for index, remainingPreference := range it.preferences {
		if index >= it.index && remainingPreference.source != PreferenceResponsesReplay {
			delete(it.preferences, index)
		}
	}
}

// Len 返回候选列表长度
func (it *Iterator) Len() int {
	return len(it.candidates)
}

// Index 返回当前迭代位置（0-based）
func (it *Iterator) Index() int {
	return it.index
}

// Skip 记录当前通道被跳过（通道禁用、无Key、类型不兼容等）
func (it *Iterator) Skip(channelID, channelKeyID int, channelName, msg string) {
	it.skip(channelID, channelKeyID, channelName, msg, CapabilityTrace{})
}

// SkipWithCapability records a planner rejection before an upstream attempt.
func (it *Iterator) SkipWithCapability(channelID, channelKeyID int, channelName, msg string, trace CapabilityTrace) {
	it.skip(channelID, channelKeyID, channelName, msg, trace)
}

func (it *Iterator) skip(channelID, channelKeyID int, channelName, msg string, trace CapabilityTrace) {
	sticky := it.IsSticky()
	it.count++
	it.attempts = append(it.attempts, model.ChannelAttempt{
		ChannelID:         channelID,
		ChannelKeyID:      channelKeyID,
		ChannelName:       channelName,
		ModelName:         it.candidates[it.index].ModelName,
		AttemptNum:        it.count,
		Status:            model.AttemptSkipped,
		Sticky:            sticky,
		Msg:               msg,
		AdapterType:       trace.AdapterType,
		CapabilityStatus:  trace.Status,
		CapabilityPolicy:  trace.Policy,
		ConversionPath:    append([]string(nil), trace.ConversionPath...),
		RequiredFeatures:  append([]string(nil), trace.RequiredFeatures...),
		DegradedFields:    append([]string(nil), trace.DegradedFields...),
		CapabilityLosses:  append([]model.CapabilityLoss(nil), trace.Losses...),
		Lossiness:         trace.Lossiness,
		CapabilityReasons: append([]string(nil), trace.Reasons...),
		FallbackReason:    msg,
	})
	it.InvalidateCurrentPreference()
}

// SkipCircuitBreak 检查熔断状态，若已熔断自动记录（含剩余冷却时间）并返回 true
func (it *Iterator) SkipCircuitBreak(channelID, channelKeyID int, channelName string) bool {
	modelName := it.candidates[it.index].ModelName
	tripped, remaining := IsTripped(channelID, channelKeyID, modelName)
	if !tripped {
		return false
	}
	msg := "circuit breaker tripped"
	if remaining > 0 {
		msg = fmt.Sprintf("circuit breaker tripped, remaining cooldown: %ds", int(remaining.Seconds()))
	}
	it.count++
	it.attempts = append(it.attempts, model.ChannelAttempt{
		ChannelID:      channelID,
		ChannelKeyID:   channelKeyID,
		ChannelName:    channelName,
		ModelName:      modelName,
		AttemptNum:     it.count,
		Status:         model.AttemptCircuitBreak,
		Sticky:         it.IsSticky(),
		Msg:            msg,
		FallbackReason: msg,
	})
	return true
}

// StartAttempt 开始一次真实转发尝试，返回 Span 用于记录结果
func (it *Iterator) StartAttempt(channelID, channelKeyID int, channelName string) *AttemptSpan {
	it.count++
	return &AttemptSpan{
		attempt: model.ChannelAttempt{
			ChannelID:    channelID,
			ChannelKeyID: channelKeyID,
			ChannelName:  channelName,
			ModelName:    it.candidates[it.index].ModelName,
			AttemptNum:   it.count,
			Sticky:       it.IsSticky(),
		},
		startTime: time.Now(),
		iter:      it,
	}
}

// Attempts 返回所有决策记录（交给日志模块持久化）
func (it *Iterator) Attempts() []model.ChannelAttempt {
	return it.attempts
}

// AttemptSpan 管理单次通道尝试的生命周期（计时、状态、结果）
type AttemptSpan struct {
	attempt   model.ChannelAttempt
	startTime time.Time
	iter      *Iterator
	ended     bool
}

// End 结束尝试：设置状态，自动计算耗时，追加到 Iterator
func (s *AttemptSpan) End(status model.AttemptStatus, statusCode int, msg string) {
	if s.ended {
		return
	}
	s.ended = true
	s.attempt.Status = status
	s.attempt.Duration = int(time.Since(s.startTime).Milliseconds())
	s.attempt.Msg = msg
	if status == model.AttemptFailed && msg != "" {
		s.attempt.FallbackReason = msg
	}
	s.iter.attempts = append(s.iter.attempts, s.attempt)
}

// SetAdapterType records the outbound protocol selected for this attempt.
func (s *AttemptSpan) SetAdapterType(adapterType string) {
	s.attempt.AdapterType = adapterType
}

// SetCapability records the semantic planning decision that authorized this attempt.
func (s *AttemptSpan) SetCapability(trace CapabilityTrace) {
	if trace.AdapterType != "" {
		s.attempt.AdapterType = trace.AdapterType
	}
	s.attempt.CapabilityStatus = trace.Status
	s.attempt.CapabilityPolicy = trace.Policy
	s.attempt.ConversionPath = append([]string(nil), trace.ConversionPath...)
	s.attempt.RequiredFeatures = append([]string(nil), trace.RequiredFeatures...)
	s.attempt.DegradedFields = append([]string(nil), trace.DegradedFields...)
	s.attempt.CapabilityLosses = append([]model.CapabilityLoss(nil), trace.Losses...)
	s.attempt.Lossiness = trace.Lossiness
	s.attempt.CapabilityReasons = append([]string(nil), trace.Reasons...)
}

// Duration 返回从开始到现在的耗时
func (s *AttemptSpan) Duration() time.Duration {
	return time.Since(s.startTime)
}
