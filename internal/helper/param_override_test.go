package helper

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func applyOverrideForTest(t *testing.T, body, override string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyParamOverride(req, &override); err != nil {
		t.Fatalf("ApplyParamOverride() error = %v", err)
	}
	modified, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(modified, &result); err != nil {
		t.Fatalf("decode modified body: %v", err)
	}
	return result
}

func TestApplyParamOverrideStructuredOperations(t *testing.T) {
	result := applyOverrideForTest(t, `{"model":"alias","metadata":{"trace":"old"},"items":[1,2]}`, `[
		{"op":"replace","path":"/model","value":"upstream"},
		{"op":"remove","path":"/metadata/trace"},
		{"op":"add","path":"/metadata/request_id","value":"abc"},
		{"op":"add","path":"/items/1","value":1.5},
		{"op":"add","path":"/items/-","value":3}
	]`)
	if result["model"] != "upstream" {
		t.Fatalf("model = %#v", result["model"])
	}
	metadata := result["metadata"].(map[string]any)
	if _, ok := metadata["trace"]; ok || metadata["request_id"] != "abc" {
		t.Fatalf("metadata = %#v", metadata)
	}
	items := result["items"].([]any)
	if len(items) != 4 || items[1] != 1.5 || items[3] != float64(3) {
		t.Fatalf("items = %#v", items)
	}
}

func TestApplyParamOverrideCopyAndLegacyMerge(t *testing.T) {
	result := applyOverrideForTest(t, `{"temperature":1,"metadata":{"trace":"abc"}}`, `{"temperature":0.2,"max_tokens":7}`)
	if result["temperature"] != 0.2 || result["max_tokens"] != float64(7) {
		t.Fatalf("legacy merge result = %#v", result)
	}
	result = applyOverrideForTest(t, `{"metadata":{"trace":"abc"}}`, `[{"op":"copy","from":"/metadata/trace","path":"/trace_id"}]`)
	if result["trace_id"] != "abc" {
		t.Fatalf("copy result = %#v", result)
	}
}
