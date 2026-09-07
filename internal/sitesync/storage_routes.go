package sitesync

import (
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func inferSiteModelRouteType(item model.SiteModel) model.SiteModelRouteType {
	return model.InferSiteModelRouteType(item.ModelName)
}

func markRouteMetadataGuessed(rawPayload string, routeType model.SiteModelRouteType) string {
	metadata, ok := model.ParseSiteModelRouteMetadata(rawPayload)
	if !ok {
		metadata = &model.SiteModelRouteMetadata{}
	}
	metadata.RouteType = routeType
	metadata.RouteSupported = true
	metadata.RouteGuessed = true
	return metadata.Marshal()
}

func applyPersistedRouteState(item *model.SiteModel, existing *model.SiteModel, now time.Time) {
	if item == nil {
		return
	}

	if canPreservePersistedRouteState(existing) {
		item.RouteType = model.NormalizeSiteModelRouteType(existing.RouteType)
		item.RouteSource = model.NormalizeSiteModelRouteSource(existing.RouteSource, existing.ManualOverride)
		item.ManualOverride = existing.ManualOverride
		item.RouteRawPayload = existing.RouteRawPayload
		item.RouteUpdatedAt = existing.RouteUpdatedAt
		return
	}

	if routeType, routeRawPayload, explicit := resolveExplicitSyncRoute(item, existing); explicit {
		metadata, hasMetadata := model.ParseSiteModelRouteMetadata(routeRawPayload)
		explicitSupportedMetadata := hasMetadata && metadata.RouteSupported &&
			model.IsProjectedSiteModelRouteType(metadata.RouteType) && !isRemovedRouteMetadata(metadata)
		removedEvidence := hasRemovedRouteEvidence(item, existing, routeRawPayload)
		removedModel := model.ContainsRemovedSiteModelRouteMarker(item.ModelName)
		if removedEvidence || (removedModel && !item.ManualOverride && !explicitSupportedMetadata) {
			// Historical route type/endpoint evidence wins over a fresh name guess.
			// A manually selected supported route (or explicit supported metadata)
			// remains valid even when its model name contains a legacy provider name.
			routeType = model.SiteModelRouteTypeUnknown
			routeRawPayload = removedRouteMetadata(routeRawPayload)
		} else if !model.IsProjectedSiteModelRouteType(routeType) {
			// 最后一步：历史遗留的未识别路由按模型名称猜测，避免模型停留在待人工指定状态。
			routeType = model.InferSiteModelRouteType(item.ModelName)
			routeRawPayload = markRouteMetadataGuessed(routeRawPayload, routeType)
		}
		item.RouteType = routeType
		item.RouteSource = model.SiteModelRouteSourceSyncInferred
		item.ManualOverride = false
		item.RouteRawPayload = routeRawPayload
		if existing != nil &&
			model.NormalizeSiteModelRouteType(existing.RouteType) == routeType &&
			strings.TrimSpace(existing.RouteRawPayload) == strings.TrimSpace(routeRawPayload) &&
			!existing.ManualOverride &&
			existing.RouteSource == model.SiteModelRouteSourceSyncInferred {
			item.RouteUpdatedAt = existing.RouteUpdatedAt
			return
		}
		item.RouteUpdatedAt = &now
		return
	}
	if isRemovedSiteModelRoute(item, existing, item.RouteRawPayload) {
		item.RouteType = model.SiteModelRouteTypeUnknown
		item.RouteSource = model.SiteModelRouteSourceSyncInferred
		item.ManualOverride = false
		rawPayload := strings.TrimSpace(item.RouteRawPayload)
		if rawPayload == "" && existing != nil {
			rawPayload = strings.TrimSpace(existing.RouteRawPayload)
		}
		item.RouteRawPayload = removedRouteMetadata(rawPayload)
		if existing != nil &&
			model.NormalizeSiteModelRouteType(existing.RouteType) == model.SiteModelRouteTypeUnknown &&
			strings.TrimSpace(existing.RouteRawPayload) == strings.TrimSpace(item.RouteRawPayload) &&
			!existing.ManualOverride &&
			existing.RouteSource == model.SiteModelRouteSourceSyncInferred {
			item.RouteUpdatedAt = existing.RouteUpdatedAt
			return
		}
		item.RouteUpdatedAt = &now
		return
	}

	item.RouteType = inferSiteModelRouteType(*item)
	item.RouteSource = model.SiteModelRouteSourceSyncInferred
	item.ManualOverride = false
	item.RouteRawPayload = ""
	if existing != nil &&
		model.NormalizeSiteModelRouteType(existing.RouteType) == item.RouteType &&
		strings.TrimSpace(existing.RouteRawPayload) == "" &&
		!existing.ManualOverride &&
		existing.RouteSource == model.SiteModelRouteSourceSyncInferred {
		item.RouteUpdatedAt = existing.RouteUpdatedAt
		return
	}
	item.RouteUpdatedAt = &now
}

func canPreservePersistedRouteState(existing *model.SiteModel) bool {
	if existing == nil || (!existing.ManualOverride && existing.RouteSource != model.SiteModelRouteSourceRuntimeLearned) {
		return false
	}
	routeType := model.NormalizeSiteModelRouteType(existing.RouteType)
	if model.IsRemovedSiteModelRouteType(existing.RouteType) || !model.IsProjectedSiteModelRouteType(routeType) {
		return false
	}
	if model.ContainsRemovedSiteModelRouteMarker(existing.RouteRawPayload) {
		return false
	}

	metadata, hasMetadata := model.ParseSiteModelRouteMetadata(existing.RouteRawPayload)
	if hasMetadata {
		if !metadata.RouteSupported || !model.IsProjectedSiteModelRouteType(metadata.RouteType) || isRemovedRouteMetadata(metadata) {
			return false
		}
	}
	if model.ContainsRemovedSiteModelRouteMarker(existing.ModelName) && !existing.ManualOverride {
		// Runtime-learned legacy names are not authoritative without explicit
		// supported metadata. A manual override is an intentional user choice.
		return hasMetadata && metadata.RouteSupported && model.IsProjectedSiteModelRouteType(metadata.RouteType)
	}
	return true
}

func hasRemovedRouteEvidence(item *model.SiteModel, existing *model.SiteModel, rawPayload string) bool {
	if item != nil && model.IsRemovedSiteModelRouteType(item.RouteType) {
		return true
	}
	if existing != nil && model.IsRemovedSiteModelRouteType(existing.RouteType) {
		return true
	}
	if model.ContainsRemovedSiteModelRouteMarker(rawPayload) {
		return true
	}
	if item != nil && model.ContainsRemovedSiteModelRouteMarker(item.RouteRawPayload) {
		return true
	}
	return existing != nil && model.ContainsRemovedSiteModelRouteMarker(existing.RouteRawPayload)
}

func isRemovedSiteModelRoute(item *model.SiteModel, existing *model.SiteModel, rawPayload string) bool {
	if item == nil {
		return false
	}
	if hasRemovedRouteEvidence(item, existing, rawPayload) {
		return true
	}
	if model.ContainsRemovedSiteModelRouteMarker(item.ModelName) {
		return true
	}
	return false
}

func removedRouteMetadata(rawPayload string) string {
	metadata, ok := model.ParseSiteModelRouteMetadata(rawPayload)
	if !ok {
		metadata = &model.SiteModelRouteMetadata{}
	}
	metadata.RouteSupported = false
	metadata.RouteGuessed = false
	metadata.RouteType = model.SiteModelRouteTypeUnknown
	if strings.TrimSpace(metadata.UnsupportedReason) == "" {
		metadata.UnsupportedReason = "Volcengine/Ark route support has been removed"
	}
	return metadata.Marshal()
}

func isRemovedRouteMetadata(metadata *model.SiteModelRouteMetadata) bool {
	if metadata == nil {
		return false
	}
	if model.ContainsRemovedSiteModelRouteMarker(metadata.UnsupportedReason) {
		return true
	}
	for _, endpoint := range append(append([]string{}, metadata.SupportedEndpointTypes...), metadata.NormalizedEndpointTypes...) {
		if model.ContainsRemovedSiteModelRouteMarker(endpoint) {
			return true
		}
	}
	return false
}

func resolveExplicitSyncRoute(item *model.SiteModel, existing *model.SiteModel) (model.SiteModelRouteType, string, bool) {
	if item != nil {
		if metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload); ok {
			return metadata.RouteType, item.RouteRawPayload, true
		}
		if strings.TrimSpace(string(item.RouteType)) != "" {
			routeType := model.NormalizeSiteModelRouteType(item.RouteType)
			return routeType, strings.TrimSpace(item.RouteRawPayload), true
		}
	}
	if existing != nil {
		if metadata, ok := model.ParseSiteModelRouteMetadata(existing.RouteRawPayload); ok {
			return metadata.RouteType, existing.RouteRawPayload, true
		}
	}
	return "", "", false
}
