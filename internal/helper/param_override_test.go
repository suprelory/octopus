package helper

import (
	"bytes"
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

func TestApplyParamOverrideWithPayloadReturnsRequestBody(t *testing.T) {
	body := `{"model":"alias","temperature":1}`
	override := `{"model":"upstream","max_tokens":7}`
	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	payload, captured, err := ApplyParamOverrideWithPayload(req, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverrideWithPayload() error = %v", err)
	}
	if !captured {
		t.Fatal("expected override path to capture the final payload")
	}
	assertRequestPayload(t, req, payload)

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode returned payload: %v", err)
	}
	if decoded["model"] != "upstream" || decoded["max_tokens"] != float64(7) {
		t.Fatalf("returned payload = %#v", decoded)
	}
}

func TestApplyParamOverrideWithPayloadReturnsStructuredBody(t *testing.T) {
	body := `{"model":"alias","metadata":{"trace":"old"}}`
	override := `[{"op":"replace","path":"/model","value":"upstream"},{"op":"remove","path":"/metadata/trace"}]`
	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	payload, captured, err := ApplyParamOverrideWithPayload(req, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverrideWithPayload() error = %v", err)
	}
	if !captured {
		t.Fatal("expected structured override path to capture the final payload")
	}
	assertRequestPayload(t, req, payload)

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode returned payload: %v", err)
	}
	if decoded["model"] != "upstream" {
		t.Fatalf("returned model = %#v", decoded["model"])
	}
	if metadata := decoded["metadata"].(map[string]any); len(metadata) != 0 {
		t.Fatalf("returned metadata = %#v", metadata)
	}
}

func TestApplyParamOverrideWithPayloadReturnsOriginalForInvalidOverride(t *testing.T) {
	body := `{"model":"alias"}`
	override := `{invalid`
	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	payload, captured, err := ApplyParamOverrideWithPayload(req, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverrideWithPayload() error = %v", err)
	}
	if !captured {
		t.Fatal("expected non-empty override path to capture the original payload")
	}
	if string(payload) != body {
		t.Fatalf("payload = %q, want %q", payload, body)
	}
	assertRequestPayload(t, req, payload)
}

func TestApplyParamOverrideWithPayloadSkipsEmptyOverride(t *testing.T) {
	body := `{"model":"alias"}`
	override := "  "
	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	payload, captured, err := ApplyParamOverrideWithPayload(req, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverrideWithPayload() error = %v", err)
	}
	if captured || payload != nil {
		t.Fatalf("expected empty override to leave body uncaptured, got captured=%t payload=%q", captured, payload)
	}
	actual, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != body {
		t.Fatalf("request body = %q, want %q", actual, body)
	}
}

func assertRequestPayload(t *testing.T, req *http.Request, want []byte) {
	t.Helper()
	if req.ContentLength != int64(len(want)) {
		t.Fatalf("content length = %d, want %d", req.ContentLength, len(want))
	}
	actual, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, want) {
		t.Fatalf("request body = %q, want %q", actual, want)
	}
	if req.GetBody == nil {
		t.Fatal("expected request GetBody to be set")
	}
	replay, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	replayed, err := io.ReadAll(replay)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(replayed, want) {
		t.Fatalf("replayed body = %q, want %q", replayed, want)
	}
}
