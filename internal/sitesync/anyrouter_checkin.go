package sitesync

import (
	"context"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func checkinAnyRouter(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*model.SiteCheckinResult, string, error) {
	accessToken, err := resolveAnyRouterManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return nil, accessToken, err
	}

	userID, _ := anyRouterDiscoverUserID(ctx, siteRecord, account, accessToken)
	if result, message, ok := anyRouterTryCheckinWithBearer(ctx, siteRecord, account, accessToken, userID); ok {
		return result, accessToken, nil
	} else if message != "" && !anyRouterShouldFallbackToCookieCheckin(message) {
		return &model.SiteCheckinResult{Status: model.SiteExecutionStatusFailed, Message: message}, accessToken, nil
	}

	result, message := anyRouterTryCheckinWithCookies(ctx, siteRecord, account, accessToken, userID)
	if result != nil {
		return result, accessToken, nil
	}

	alternateUserID, _ := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID)
	if alternateUserID > 0 {
		result, message = anyRouterTryCheckinWithCookies(ctx, siteRecord, account, accessToken, alternateUserID)
		if result != nil {
			return result, accessToken, nil
		}
	}

	return &model.SiteCheckinResult{
		Status:  model.SiteExecutionStatusFailed,
		Message: firstNonEmptyString(message, "checkin failed"),
	}, accessToken, nil
}

func anyRouterTryCheckinWithBearer(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (*model.SiteCheckinResult, string, bool) {
	payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodPost,
		buildSiteURL(siteRecord.BaseURL, "/api/user/checkin"),
		nil,
		anyRouterAuthHeaders(accessToken, userID),
		account,
	)
	if err != nil {
		return nil, err.Error(), false
	}
	if payload == nil {
		return nil, "", false
	}
	if result, ok := anyRouterBuildCheckinResult(payload); ok {
		return result, result.Message, true
	}
	return nil, anyRouterExtractResponseMessage(payload), false
}

func anyRouterTryCheckinWithCookies(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (*model.SiteCheckinResult, string) {
	firstFailure := ""
	for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
		signInPayload, _, signInErr := anyRouterRequestJSONWithCookies(
			ctx,
			siteRecord,
			http.MethodPost,
			buildSiteURL(siteRecord.BaseURL, "/api/user/sign_in"),
			map[string]any{},
			map[string]string{
				"Cookie":           cookie,
				"X-Requested-With": "XMLHttpRequest",
			},
			account,
		)
		if signInErr == nil && signInPayload != nil {
			if result, ok := anyRouterBuildCheckinResult(signInPayload); ok {
				return result, result.Message
			}
			if message := anyRouterExtractResponseMessage(signInPayload); message != "" && firstFailure == "" {
				firstFailure = message
			}
		} else if signInErr != nil && firstFailure == "" {
			firstFailure = signInErr.Error()
		}

		headers := map[string]string{"Cookie": cookie}
		anyRouterAddUserIDHeaders(headers, userID)
		payload, _, err := anyRouterRequestJSONWithCookies(
			ctx,
			siteRecord,
			http.MethodPost,
			buildSiteURL(siteRecord.BaseURL, "/api/user/checkin"),
			nil,
			headers,
			account,
		)
		if err == nil && payload != nil {
			if result, ok := anyRouterBuildCheckinResult(payload); ok {
				return result, result.Message
			}
			if message := anyRouterExtractResponseMessage(payload); message != "" {
				firstFailure = message
			}
			continue
		}
		if err != nil {
			firstFailure = err.Error()
		}
	}

	return nil, firstNonEmptyString(firstFailure, "checkin failed")
}

func anyRouterBuildCheckinResult(payload map[string]any) (*model.SiteCheckinResult, bool) {
	if payload == nil {
		return nil, false
	}
	message := firstNonEmptyString(anyRouterExtractResponseMessage(payload), "checkin success")
	if jsonBool(payload["success"]) || isAlreadyCheckedInMessage(message) {
		return &model.SiteCheckinResult{
			Status:  model.SiteExecutionStatusSuccess,
			Message: message,
			Reward:  jsonString(nestedValue(payload, "data", "reward")),
		}, true
	}
	return nil, false
}

func anyRouterShouldFallbackToCookieCheckin(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return true
	}
	return strings.Contains(text, "unexpected token") ||
		strings.Contains(text, "not valid json") ||
		strings.Contains(text, "<html") ||
		strings.Contains(text, "new-api-user") ||
		strings.Contains(text, "access token") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "not login") ||
		strings.Contains(text, "not logged") ||
		strings.Contains(text, "invalid url (post /api/user/checkin)") ||
		(strings.Contains(text, "http 404") && strings.Contains(text, "/api/user/checkin")) ||
		strings.Contains(text, "未登录") ||
		strings.Contains(text, "未提供")
}
