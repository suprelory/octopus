package helper

import "testing"

func TestExtractHTMLTitlePreservesErrorReason(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`<html><title>upstream.example | 502: Bad gateway</title></html>`, "502: Bad gateway"},
		{`<html><title>upstream.example &#124; 503: Service Unavailable</title></html>`, "503: Service Unavailable"},
		{`<html><title>Cloudflare Tunnel error | Cloudflare</title></html>`, "Cloudflare Tunnel error"},
		{`<html><title>Maintenance | upstream.example</title></html>`, "Maintenance | upstream.example"},
		{`<TITLE class="error">502 Bad Gateway</TITLE>`, "502 Bad Gateway"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := ExtractHTMLTitle(tc.body); got != tc.want {
				t.Fatalf("title = %q, want %q", got, tc.want)
			}
			if got := formatModelHTTPError(502, "text/html", []byte(tc.body)).Error(); got != "http 502: "+tc.want {
				t.Fatalf("model error lost its title: %q", got)
			}
		})
	}
}

func TestOrdinaryWaitingMessageIsNotCloudflareProtection(t *testing.T) {
	for _, message := range []string{
		"Please wait just a moment",
		`<html><body>Please wait just a moment</body></html>`,
		`<html><title>Maintenance</title><body>Please wait just a moment</body></html>`,
	} {
		if IsCloudflareProtectionMessage(message) {
			t.Fatalf("ordinary waiting message was classified as a challenge: %q", message)
		}
		if got := formatModelHTTPError(503, "text/html", []byte(message)).Error(); got == "http 503: Just a moment..." {
			t.Fatalf("ordinary waiting message became a challenge summary: %q", got)
		}
	}
}
