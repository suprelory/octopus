package helper

import (
	"regexp"
	"strings"
)

var cloudflareWaitingTitlePattern = regexp.MustCompile(`(?i)(?:^|:\s*)just a moment[.!\s…]*$`)

// IsCloudflareProtectionMessage recognizes explicit challenge and blocking
// markers. Cloudflare branding also appears on ordinary upstream error pages.
func IsCloudflareProtectionMessage(message string) bool {
	lowered := strings.ToLower(message)
	for _, marker := range []string{
		"/cdn-cgi/challenge-platform/",
		"cf-chl-",
		"_cf_chl_opt",
		"cloudflare challenge",
		"cloudflare protection",
		"cloudflare 保护",
		"cloudflare 保護",
	} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	if cloudflareWaitingTitlePattern.MatchString(lowered) || cloudflareWaitingTitlePattern.MatchString(ExtractHTMLTitle(message)) {
		return true
	}
	return strings.Contains(lowered, "cloudflare") &&
		(strings.Contains(lowered, "attention required") || strings.Contains(lowered, "sorry, you have been blocked"))
}
