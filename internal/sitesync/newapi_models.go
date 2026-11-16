package sitesync

import (
	"context"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func isNewAPIModelEndpointMissing(err error) bool {
	status := siteErrorStatusCode(err)
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed
}

func fetchNewAPIModelsForToken(ctx context.Context, site *model.Site, account *model.SiteAccount, token model.SiteToken) ([]string, error) {
	if !isUsableSiteToken(token) {
		return nil, newMissingGroupKeyError(token.GroupKey)
	}
	base := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	canonical := base
	if !strings.HasSuffix(strings.ToLower(base), "/v1") {
		canonical += "/v1"
	}
	headers := map[string]string{"Authorization": ensureBearer(model.NormalizeSiteSyncTokenValueForPlatform(site.Platform, token.Token))}
	payload, err := requestJSON(ctx, site, http.MethodGet, canonical+"/models", nil, headers, account)
	if isNewAPIModelEndpointMissing(err) && canonical != base {
		legacy, legacyErr := requestJSON(ctx, site, http.MethodGet, base+"/models", nil, headers, account)
		if legacyErr == nil {
			payload, err = legacy, nil
		}
		if legacyErr != nil && siteErrorStatusCode(legacyErr) != 0 && !isNewAPIModelEndpointMissing(legacyErr) {
			return nil, legacyErr
		}
	}
	if err != nil {
		return nil, err
	}
	if err := validateSiteBusinessResponse(payload); err != nil {
		return nil, err
	}
	items, ok := payload["data"].([]any)
	if !ok {
		return nil, wrapSiteDecodeError("new-api models response is missing its data list", nil)
	}
	names := make([]string, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || jsonString(item["id"]) == "" {
			return nil, wrapSiteDecodeError("new-api models response contains an invalid model", nil)
		}
		names = append(names, jsonString(item["id"]))
	}
	return normalizeModelNames(names), nil
}

// Pricing describes physical upstream groups. A token's successful model list
// already accounts for inherited/auto groups and is authoritative for its local
// group, including the synthetic group used by direct API-key accounts.
func confirmNewAPITokenModelGroups(items []model.SiteModel) []model.SiteModel {
	for i := range items {
		items[i].TokenGroupVerified = true
		metadata, ok := model.ParseSiteModelRouteMetadata(items[i].RouteRawPayload)
		if !ok {
			continue
		}
		metadata.VerifiedTokenGroup = model.NormalizeSiteGroupKey(items[i].GroupKey)
		items[i].RouteRawPayload = metadata.Marshal()
	}
	return items
}
