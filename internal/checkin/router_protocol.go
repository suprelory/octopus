package checkin

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func routerSuccess(data any) bool {
	recognized := false
	for _, rule := range []model.CheckinMatch{{Path: "success", Value: json.RawMessage("true")}, {Path: "ret", Value: json.RawMessage("1")}, {Path: "code", Value: json.RawMessage("0")}} {
		if _, exists := field(data, rule.Path); exists {
			if !matches(data, rule) {
				return false
			}
			recognized = true
		}
	}
	return recognized
}

func routerMessage(data any) string {
	var messages []string
	for _, path := range []string{"message", "msg", "error.message", "error"} {
		if value, _ := field(data, path); value != nil {
			if message, ok := value.(string); ok {
				messages = append(messages, strings.ToLower(message))
			}
		}
	}
	return strings.Join(messages, " ")
}

func routerChallenge(response HTTPResult, data any) bool {
	body := bytes.ToLower(response.Body)
	for _, marker := range []string{"cloudflare", "turnstile", "cf-chl-", "acw_sc__", "var arg1=", "captcha"} {
		if bytes.Contains(body, []byte(marker)) {
			return true
		}
	}
	for _, path := range []string{"data.require_2fa", "data.requires_2fa", "data.two_factor_required", "data.requires_verification", "data.verification_required", "data.require_verification"} {
		if value, _ := field(data, path); value == true {
			return true
		}
	}
	for _, path := range []string{"data.challenge", "data.challenge_id", "data.flow_token"} {
		if value, exists := field(data, path); exists && value != nil && value != "" {
			return true
		}
	}
	message := routerMessage(data)
	return strings.Contains(message, "验证码") || strings.Contains(message, "人机验证") || strings.Contains(message, "two-factor")
}

func routerAuthMismatch(data any) bool {
	message := routerMessage(data)
	for _, marker := range []string{"new-api-user", "access token", "access_token", "unauthorized", "forbidden", "not login", "not logged", "invalid token", "session expired", "未登录", "未登錄", "登录失效", "登入失效", "无权进行", "無權進行"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// Another POST is allowed only after an explicit rejection. Unknown responses,
// contradictory statuses, HTML, challenges, timeouts, 429 and 5xx always stop.
func routerCanFallback(response HTTPResult) bool {
	if response.Err != nil || response.StatusCode == 429 || response.StatusCode >= 500 {
		return false
	}
	data, err := decodeObject(response.Body)
	if routerChallenge(response, data) || bytes.Contains(bytes.ToLower(response.Body), []byte("<html")) || bytes.Contains(bytes.ToLower(response.Body), []byte("<!doctype")) {
		return false
	}
	for _, rule := range []model.CheckinMatch{{Path: "success", Value: json.RawMessage("true")}, {Path: "ret", Value: json.RawMessage("1")}, {Path: "code", Value: json.RawMessage("0")}} {
		if matches(data, rule) {
			return false
		}
	}
	if response.StatusCode == 404 || response.StatusCode == 405 {
		return true
	}
	if err != nil {
		return false
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("false")}) || routerAuthMismatch(data)
	}
	return response.StatusCode >= 200 && response.StatusCode < 300 && routerAuthMismatch(data) && matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("false")})
}

func routerSubmitResult(response HTTPResult) SubmitResult {
	result := SubmitResult{Status: "manual_required", Reason: "unconfirmed_response"}
	data, err := decodeObject(response.Body)
	if response.Err != nil {
		result.Reason = "network_error"
		return result
	}
	if routerChallenge(response, data) {
		result.Reason = "verification_required"
		return result
	}
	if response.StatusCode == 429 {
		result.Reason = "rate_limited"
		return result
	}
	if response.StatusCode >= 500 {
		result.Reason = "upstream_http_error"
		return result
	}
	if response.StatusCode == 401 || response.StatusCode == 403 || (err == nil && routerAuthMismatch(data) && !routerSuccess(data)) {
		result.Reason, result.SafeToRetry = "invalid_credentials", routerCanFallback(response)
		return result
	}
	if response.StatusCode == 404 || response.StatusCode == 405 {
		if routerCanFallback(response) {
			return SubmitResult{Status: "failed", Reason: "unsupported_endpoint", SafeToRetry: true}
		}
		return result
	}
	if err != nil {
		return result
	}
	// Already-checked-in is affirmative date evidence, even if success:false is
	// used for this business rejection. It never produces a new reward.
	if interpreted := interpretSubmit(model.CheckinSiteConfig{}, response.StatusCode, response.Body); interpreted.Status == "already_checked_in" {
		return interpreted
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && routerSuccess(data) {
		return SubmitResult{Status: "success", Reason: "checked_in"}
	}
	return result
}
