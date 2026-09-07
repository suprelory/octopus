package sitesync

import (
	"context"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func syncProjectedModelPrices(ctx context.Context, modelsByGroup map[string][]model.SiteModel) error {
	modelNames := make([]string, 0)
	seen := make(map[string]struct{})
	for _, groupModels := range modelsByGroup {
		for _, item := range groupModels {
			modelName := strings.TrimSpace(item.ModelName)
			if modelName == "" {
				continue
			}
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			modelNames = append(modelNames, modelName)
		}
	}
	if len(modelNames) == 0 {
		return nil
	}
	return helper.LLMPriceAddToDB(modelNames, ctx)
}

func platformOutboundType(site *model.Site) outbound.OutboundType {
	if site.Platform == model.SitePlatformAPI {
		switch site.ResolveDefaultRouteType() {
		case model.SiteModelRouteTypeAnthropic:
			return outbound.OutboundTypeAnthropic
		case model.SiteModelRouteTypeGemini:
			return outbound.OutboundTypeGemini
		case model.SiteModelRouteTypeUnknown:
			return outbound.OutboundTypeUnsupported
		default:
			return outbound.OutboundTypeOpenAIChat
		}
	}
	return outbound.OutboundTypeOpenAIChat
}

func partitionSiteModelsByRouteType(items []model.SiteModel, split bool, site *model.Site) map[model.SiteModelRouteType][]model.SiteModel {
	if !split {
		routeType := model.SiteModelRouteTypeFromOutboundType(platformOutboundType(site))
		if len(items) == 0 || !model.IsProjectedSiteModelRouteType(routeType) {
			return map[model.SiteModelRouteType][]model.SiteModel{}
		}
		projectable := make([]model.SiteModel, 0, len(items))
		for _, item := range items {
			// Explicitly unsupported route metadata and retired provider rows must
			// not be silently folded into the site's default OpenAI Chat channel.
			if _, ok := projectableSiteModelRouteType(item); !ok {
				continue
			}
			projectable = append(projectable, item)
		}
		if len(projectable) == 0 {
			return map[model.SiteModelRouteType][]model.SiteModel{}
		}
		return map[model.SiteModelRouteType][]model.SiteModel{routeType: projectable}
	}
	buckets := make(map[model.SiteModelRouteType][]model.SiteModel)
	for _, item := range items {
		routeType, ok := projectableSiteModelRouteType(item)
		if !ok {
			continue
		}
		buckets[routeType] = append(buckets[routeType], item)
	}
	return buckets
}

// projectableSiteModelRouteType resolves a persisted model's route without
// allowing historical Volcengine/Ark rows to fall back to OpenAI Chat. Empty
// route values retain the existing model-name inference for ordinary models;
// explicit supported metadata is preferred when it is available.
func projectableSiteModelRouteType(item model.SiteModel) (model.SiteModelRouteType, bool) {
	if model.HasRemovedSiteModelRouteEvidence(item) {
		return model.SiteModelRouteTypeUnknown, false
	}
	metadata, hasMetadata := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
	if hasMetadata && !metadata.RouteSupported {
		return model.SiteModelRouteTypeUnknown, false
	}

	routeType := item.RouteType
	if strings.TrimSpace(string(routeType)) == "" {
		if hasMetadata && model.IsProjectedSiteModelRouteType(metadata.RouteType) {
			routeType = metadata.RouteType
		} else {
			routeType = model.InferSiteModelRouteType(item.ModelName)
		}
	} else {
		routeType = model.NormalizeSiteModelRouteType(routeType)
	}
	if !model.IsProjectedSiteModelRouteType(routeType) {
		return model.SiteModelRouteTypeUnknown, false
	}
	return routeType, true
}

func compactSiteModels(items []model.SiteModel) []model.SiteModel {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]model.SiteModel, 0, len(items))
	for _, item := range items {
		key := model.NormalizeSiteGroupKey(item.GroupKey) + "\x00" + strings.TrimSpace(item.ModelName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b model.SiteModel) int {
		return strings.Compare(a.ModelName, b.ModelName)
	})
	return result
}

func extractSiteModelNames(items []model.SiteModel) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.ModelName)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func siteModelBelongsToProjectedGroup(item model.SiteModel, groupKey string) bool {
	metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
	if !ok || len(metadata.EnableGroups) == 0 {
		return true
	}
	targetGroupKey := model.NormalizeSiteGroupKey(groupKey)
	for _, explicitGroupKey := range metadata.EnableGroups {
		if model.NormalizeSiteGroupKey(explicitGroupKey) == targetGroupKey {
			return true
		}
	}
	return false
}

// compositeBindingKey 生成复合绑定 key，用于区分同一 tokenGroup 的不同端点格式 Channel
func compositeBindingKey(groupKey string, obType outbound.OutboundType, split bool) string {
	return model.ComposeSiteChannelBindingKey(groupKey, model.SiteModelRouteTypeFromOutboundType(obType), split)
}

func parseCompositeBindingKey(groupKey string) (string, model.SiteModelRouteType) {
	return model.ParseSiteChannelBindingKey(groupKey)
}
