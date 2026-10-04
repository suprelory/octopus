package op

import (
	"strings"
	"testing"
)

func TestRawUsageRetainsProviderFieldsAcrossProtocols(t *testing.T) {
	for _, tc := range []struct{ body, ct, path, want string }{
		{`{"output":"not a usage object","usage":{"input_tokens":4,"input_tokens_details":{"cached_tokens":3},"custom":true}}`, "application/json", "usage", `"custom":true`},
		{`{"usageMetadata":{"promptTokenCount":3,"thoughtsTokenCount":7}}`, "application/json", "usageMetadata", `"thoughtsTokenCount":7`},
		{"event: message_start\r\ndata: {\"message\":{\"usage\":{\"cache_creation_input_tokens\":8}}}\r\n\r\n", "text/event-stream", "message.usage", `"cache_creation_input_tokens":8`},
		{`{"type":"response.created"}{"type":"response.completed","response":{"usage":{"total_tokens":19}}}`, "application/json", "response.usage", `"total_tokens":19`},
	} {
		got := extractRelayRawUsage([]byte(tc.body), tc.ct)
		if len(got) != 1 || got[0].Path != tc.path || !strings.Contains(string(got[0].Value), tc.want) {
			t.Fatalf("%s: %+v", tc.body, got)
		}
	}
}

func TestRawUsageIgnoresEmbeddedTextAndFindsLongStreamTail(t *testing.T) {
	body := strings.Repeat("data: {\"delta\":\"usage: {\\\"tokens\\\":99}\"}\n\n", 1000) + "data: {\"usage\":{\"output_tokens\":42}}\n\n"
	got := extractRelayRawUsage([]byte(body), "text/event-stream")
	if len(got) != 1 || got[0].Event != 1001 {
		t.Fatalf("%+v", got)
	}
	body = strings.Repeat("data: {\"usage\":{\"tokens\":1}}\n\n", 30) + "data: {\"usage\":{\"tokens\":99}}\n\n"
	got = extractRelayRawUsage([]byte(body), "text/event-stream")
	if len(got) != 16 || got[15].Event != 31 || string(got[15].Value) != `{"tokens":99}` {
		t.Fatalf("%+v", got)
	}
}
