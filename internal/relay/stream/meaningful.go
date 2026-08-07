package stream

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tmaxmax/go-sse"
)

// IsMeaningfulPayload recognizes semantic content across the inbound formats
// supported by relay. Metadata-only events (created/start/usage/done) remain
// buffered so a failed upstream can still be retried safely.
func IsMeaningfulPayload(_, transformed []byte) bool {
	trimmed := bytes.TrimSpace(transformed)
	if len(trimmed) == 0 {
		return false
	}

	foundSSE := false
	readConfig := &sse.ReadConfig{MaxEventSize: 64 * 1024 * 1024}
	// Keep the trailing blank line: it is the SSE event delimiter.
	for event, err := range sse.Read(bytes.NewReader(transformed), readConfig) {
		if err != nil {
			break
		}
		foundSSE = true
		if meaningfulJSON([]byte(event.Data)) {
			return true
		}
	}
	if foundSSE {
		return false
	}
	return meaningfulJSON(trimmed)
}

func meaningfulJSON(data []byte) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return false
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		return false
	}

	if choices, ok := payload["choices"].([]any); ok {
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			if meaningfulMap(mapValue(choice, "delta")) || meaningfulMap(mapValue(choice, "message")) {
				return true
			}
		}
	}

	eventType, _ := payload["type"].(string)
	switch {
	case strings.Contains(eventType, ".delta"):
		if meaningfulScalar(payload["delta"]) || meaningfulMap(mapValue(payload, "delta")) {
			return true
		}
	case eventType == "content_block_start":
		block := mapValue(payload, "content_block")
		blockType, _ := block["type"].(string)
		return strings.Contains(blockType, "tool_use")
	case eventType == "response.output_item.added":
		item := mapValue(payload, "item")
		itemType, _ := item["type"].(string)
		return strings.Contains(itemType, "function_call") || strings.Contains(itemType, "tool")
	}

	if candidates, ok := payload["candidates"].([]any); ok {
		for _, rawCandidate := range candidates {
			candidate, _ := rawCandidate.(map[string]any)
			content := mapValue(candidate, "content")
			if meaningfulSlice(content["parts"]) {
				return true
			}
		}
	}
	return false
}

func meaningfulMap(value map[string]any) bool {
	if value == nil {
		return false
	}
	for _, key := range []string{"content", "text", "reasoning", "reasoning_content", "thinking", "partial_json", "arguments", "function_call"} {
		if meaningfulScalar(value[key]) {
			return true
		}
	}
	for _, key := range []string{"tool_calls", "function_calls", "parts", "images", "audio"} {
		if meaningfulSlice(value[key]) || meaningfulScalar(value[key]) {
			return true
		}
	}
	return false
}

func meaningfulSlice(value any) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		if meaningfulScalar(item) {
			return true
		}
	}
	return false
}

func meaningfulScalar(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return false
	}
}

func mapValue(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	value, _ := parent[key].(map[string]any)
	return value
}
