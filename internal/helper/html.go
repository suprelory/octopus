package helper

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlTitlePattern      = regexp.MustCompile(`(?is)<title\b[^>]*>([^<]*)</title>`)
	htmlMarkerPattern     = regexp.MustCompile(`(?i)<!doctype\s+html|<(?:html|head|body|title|script)\b`)
	httpErrorTitlePattern = regexp.MustCompile(`(?i)^(?:(?:http|error)\s*)?[45]\d\d\b`)
)

func IsHTMLResponse(contentType, body string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/html") || htmlMarkerPattern.MatchString(body)
}

func ExtractHTMLTitle(body string) string {
	match := htmlTitlePattern.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return NormalizeHTMLTitle(match[1])
}

// NormalizeHTMLTitle keeps the error portion of titles such as
// "example.com | 502: Bad gateway" and removes a trailing CDN brand.
func NormalizeHTMLTitle(title string) string {
	title = strings.Join(strings.Fields(html.UnescapeString(title)), " ")
	parts := strings.Split(title, "|")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if httpErrorTitlePattern.MatchString(part) {
			return part
		}
	}
	if len(parts) > 1 && strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "cloudflare") {
		return strings.TrimSpace(strings.Join(parts[:len(parts)-1], "|"))
	}
	return title
}
