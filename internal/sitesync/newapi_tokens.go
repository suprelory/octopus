package sitesync

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// Fetch the complete inventory before reconciling it with local tokens. A
// truncated or changing inventory must never be interpreted as deletions.
func fetchNewAPITokens(ctx context.Context, site *model.Site, account *model.SiteAccount, accessToken string) ([]model.SiteToken, error) {
	const pageSize = 100
	tokens := make([]model.SiteToken, 0)
	seen := make(map[string]struct{})
	total := -1
	userGroup := ""
	for page := 1; ; page++ {
		if page > 1000 {
			return nil, fmt.Errorf("new-api token pagination exceeded its limit; previous tokens were retained")
		}
		path := fmt.Sprintf("/api/token/?p=%d&size=%d&page_size=%d", page, pageSize, pageSize)
		payload, err := requestJSONWithManagedAccessToken(ctx, site, http.MethodGet, buildSiteURL(site.BaseURL, path), nil, accessToken, account)
		if err != nil {
			return nil, err
		}
		if err := validateSiteBusinessResponse(payload); err != nil {
			return nil, err
		}
		var rows []any
		for _, candidate := range itemSliceCandidates(payload) {
			if list, ok := candidate.([]any); ok {
				rows = list
				break
			}
		}
		if rows == nil {
			return nil, wrapSiteDecodeError("new-api token response is missing its item list", nil)
		}
		if data, ok := payload["data"].(map[string]any); ok {
			if value, exists := data["total"]; exists {
				count, err := strconv.Atoi(jsonString(value))
				if err != nil || count < 0 || (total >= 0 && total != count) {
					return nil, wrapSiteDecodeError("new-api token total is invalid or changed during pagination", nil)
				}
				total = count
			}
		}
		for _, row := range rows {
			item, ok := row.(map[string]any)
			if !ok {
				return nil, wrapSiteDecodeError("new-api token list contains an invalid item", nil)
			}
			id := anyRouterParseInt(item["id"])
			value := strings.TrimSpace(jsonString(item["key"]))
			if id <= 0 && (value == "" || model.IsMaskedSiteTokenValue(value)) {
				return nil, wrapSiteDecodeError("new-api token is missing its ID or full key", nil)
			}
			identity := "id:" + strconv.Itoa(id)
			if id <= 0 {
				identity = "key:" + value
			}
			if _, duplicate := seen[identity]; duplicate {
				return nil, wrapSiteDecodeError("new-api token pagination repeated an item; previous tokens were retained", nil)
			}
			seen[identity] = struct{}{}
			status := 1
			if raw, exists := item["status"]; exists {
				status = anyRouterParseInt(raw)
				if status < 1 || status > 4 {
					return nil, wrapSiteDecodeError("new-api token has an invalid status", nil)
				}
			}
			if status == 1 {
				expiration, hasExpiration := item["expired_time"]
				if expires := anyToInt64(expiration); hasExpiration && expires != -1 && expires <= time.Now().Unix() {
					status = 3
				} else if quota, present := balanceJSONNumber(item["remain_quota"]); present && !jsonBool(item["unlimited_quota"]) && quota <= 0 {
					status = 4
				}
			}
			group := jsonString(item["group"])
			if group == "" {
				if userGroup == "" {
					profile, err := requestJSONWithManagedAccessToken(ctx, site, http.MethodGet, buildSiteURL(site.BaseURL, "/api/user/self"), nil, accessToken, account)
					if err != nil {
						return nil, err
					}
					if err := validateSiteBusinessResponse(profile); err != nil {
						return nil, err
					}
					userGroup = jsonString(nestedValue(profile, "data", "group"))
					if userGroup == "" {
						return nil, wrapSiteDecodeError("new-api user profile is missing the inherited token group", nil)
					}
				}
				group = userGroup
			}
			group = model.NormalizeSiteGroupKey(group)
			token := model.SiteToken{
				UpstreamID: id, UpstreamStatus: status,
				ModelLimitsEnabled: jsonBool(item["model_limits_enabled"]),
				Name:               firstNonEmptyString(jsonString(item["name"]), fmt.Sprintf("token-%d", len(tokens)+1)),
				Token:              value, GroupKey: group, GroupName: model.NormalizeSiteGroupName(group, jsonString(item["group_name"])),
				Enabled: true, Source: "sync", IsDefault: len(tokens) == 0,
			}
			// A saved full key is usable before model discovery, not only after
			// persistence. A changed mask must never resurrect a revoked key.
			matches := make([]model.SiteToken, 0, 1)
			for _, previous := range account.Tokens {
				if !model.IsReadySiteToken(previous) || strings.TrimSpace(previous.Token) == "" {
					continue
				}
				sameID := id > 0 && previous.UpstreamID == id
				legacyMatch := previous.UpstreamID == 0 && previous.Name == token.Name && previous.GroupKey == token.GroupKey
				if (sameID || legacyMatch) && model.SiteMaskedTokenMatches(previous.Token, value) {
					matches = append(matches, previous)
				}
			}
			if len(matches) == 1 {
				token.Token = matches[0].Token
				token.Enabled = matches[0].Enabled
			}
			tokens = append(tokens, token)
		}
		if total >= 0 {
			if len(tokens) > total || (len(rows) == 0 && len(tokens) < total) {
				return nil, wrapSiteDecodeError("new-api token pagination is incomplete", nil)
			}
			if len(tokens) == total {
				break
			}
		} else if len(rows) < pageSize {
			break
		}
	}

	for start := 0; start < len(tokens); start += pageSize {
		end := min(start+pageSize, len(tokens))
		ids := make([]int, 0, end-start)
		for _, token := range tokens[start:end] {
			if model.IsSiteTokenUpstreamAvailable(token) && (token.Token == "" || model.IsMaskedSiteTokenValue(token.Token)) {
				ids = append(ids, token.UpstreamID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		payload, err := requestJSONWithManagedAccessToken(ctx, site, http.MethodPost, buildSiteURL(site.BaseURL, "/api/token/batch/keys"), map[string]any{"ids": ids}, accessToken, account)
		if err == nil {
			err = validateSiteBusinessResponse(payload)
		}
		// Older versions expose only the individual reveal endpoint.
		individual := siteErrorStatusCode(err) == http.StatusNotFound || siteErrorStatusCode(err) == http.StatusMethodNotAllowed
		if siteErrorStatusCode(err) == http.StatusForbidden {
			return nil, newSiteHTTPError(http.StatusForbidden, "new-api full key retrieval requires api_key:reveal permission")
		}
		if err != nil && !individual {
			return nil, fmt.Errorf("new-api full key retrieval failed (access tokens need api_key:reveal permission): %w", err)
		}
		keys, _ := nestedValue(payload, "data", "keys").(map[string]any)
		for i := start; i < end; i++ {
			token := &tokens[i]
			if !model.IsSiteTokenUpstreamAvailable(*token) || (token.Token != "" && !model.IsMaskedSiteTokenValue(token.Token)) {
				continue
			}
			value := jsonString(keys[strconv.Itoa(token.UpstreamID)])
			if individual {
				path := fmt.Sprintf("/api/token/%d/key", token.UpstreamID)
				payload, err := requestJSONWithManagedAccessToken(ctx, site, http.MethodPost, buildSiteURL(site.BaseURL, path), nil, accessToken, account)
				if siteErrorStatusCode(err) == http.StatusForbidden {
					return nil, newSiteHTTPError(http.StatusForbidden, "new-api full key retrieval requires api_key:reveal permission")
				}
				if err != nil {
					return nil, err
				}
				if err := validateSiteBusinessResponse(payload); err != nil {
					return nil, err
				}
				value = jsonString(nestedValue(payload, "data", "key"))
			}
			if value == "" || model.IsMaskedSiteTokenValue(value) || (token.Token != "" && !model.SiteMaskedTokenMatches(value, token.Token)) {
				return nil, wrapSiteDecodeError(fmt.Sprintf("new-api did not return a matching full key for token %d", token.UpstreamID), nil)
			}
			token.Token = value
		}
	}
	return tokens, nil
}
