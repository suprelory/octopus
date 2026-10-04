package capture

import (
	"net/http"
	"net/url"
	"strings"
)

const Redacted = "[REDACTED]"

func sensitive(name string) bool {
	name = strings.ToLower(name)
	for _, part := range []string{"authorization", "cookie", "api-key", "apikey", "api_key", "token", "secret", "password"} {
		if strings.Contains(name, part) {
			return true
		}
	}
	return name == "key" || strings.HasSuffix(name, "-auth") || strings.HasSuffix(name, "-key")
}

func Headers(source http.Header, secrets ...string) map[string][]string {
	result := make(map[string][]string, len(source))
	for name, values := range source {
		copied := append([]string(nil), values...)
		for i, value := range copied {
			if sensitive(name) {
				copied[i] = Redacted
				continue
			}
			copied[i] = Text(value, secrets...)
		}
		result[name] = copied
	}
	return result
}

func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL]"
	}
	if u.User != nil {
		u.User = url.User(Redacted)
	}
	query := u.Query()
	changed := false
	for name := range query {
		if sensitive(name) {
			query.Set(name, Redacted)
			changed = true
		}
	}
	if changed {
		u.RawQuery = query.Encode()
	}
	u.Fragment = ""
	return u.String()
}

func Text(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, Redacted)
		}
	}
	return text
}
