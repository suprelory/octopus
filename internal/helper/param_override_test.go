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

func TestInspectParamOverrideReturnsStablePathsAndFingerprint(t *testing.T) {
	override := `[{"op":"replace","path":"/model","value":"upstream"},{"op":"remove","path":"/metadata/trace"},{"op":"copy","from":"/user","path":"/metadata/user"}]`
	inspection := InspectParamOverride(&override)
	if !inspection.Active || !inspection.Valid {
		t.Fatalf("inspection = %#v, want active valid override", inspection)
	}
	if inspection.Fingerprint == "" {
		t.Fatal("expected override fingerprint")
	}
	want := []string{"/metadata/trace", "/metadata/user", "/model", "/user"}
	if len(inspection.Paths) != len(want) {
		t.Fatalf("paths = %#v, want %#v", inspection.Paths, want)
	}
	for i := range want {
		if inspection.Paths[i] != want[i] {
			t.Fatalf("paths = %#v, want %#v", inspection.Paths, want)
		}
	}

	changed := `[{"op":"replace","path":"/model","value":"other"}]`
	other := InspectParamOverride(&changed)
	if other.Fingerprint == inspection.Fingerprint {
		t.Fatal("different overrides must have different fingerprints")
	}
	canonicalA := `[{"op":"replace","path":"/model","value":"upstream"}]`
	canonicalB := ` [ { "op" : "replace", "path" : "/model", "value" : "upstream" } ] `
	if InspectParamOverride(&canonicalA).Fingerprint != InspectParamOverride(&canonicalB).Fingerprint {
		t.Fatal("equivalent override documents should have the same fingerprint")
	}
}

func TestInspectParamOverrideInvalidSyntaxIsInactive(t *testing.T) {
	override := `{invalid`
	inspection := InspectParamOverride(&override)
	if inspection.Active || inspection.Valid {
		t.Fatalf("invalid inspection = %#v, want inactive invalid", inspection)
	}
}

func TestInspectParamOverrideEmptyDocumentsAreValidButInactive(t *testing.T) {
	for _, raw := range []string{"{}", "[]"} {
		raw := raw
		inspection := InspectParamOverride(&raw)
		if !inspection.Valid || inspection.Active {
			t.Fatalf("inspection for %s = %#v, want valid inactive", raw, inspection)
		}
	}
}

func TestApplyParamOverrideEmptyDocumentsPreserveBytes(t *testing.T) {
	body := []byte("{ \"model\" : \"alias\", \"temperature\" : 1 }\n")
	for _, raw := range []string{"{}", "[]"} {
		raw := raw
		payload, captured, err := ApplyParamOverridePayload(body, &raw)
		if err != nil {
			t.Fatalf("ApplyParamOverridePayload(%s) error = %v", raw, err)
		}
		if captured {
			t.Fatalf("ApplyParamOverridePayload(%s) captured a no-op document", raw)
		}
		if !bytes.Equal(payload, body) {
			t.Fatalf("ApplyParamOverridePayload(%s) changed bytes: %q", raw, payload)
		}
	}
}

func TestApplyParamOverridePayloadMatchesRequestPath(t *testing.T) {
	body := []byte(`{"model":"alias","temperature":1}`)
	override := `{"temperature":0.2,"max_tokens":7}`
	payload, captured, err := ApplyParamOverridePayload(body, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverridePayload() error = %v", err)
	}
	if !captured {
		t.Fatal("expected payload to be captured")
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded["temperature"] != 0.2 || decoded["max_tokens"] != float64(7) {
		t.Fatalf("payload = %#v", decoded)
	}
}

func TestApplyParamOverrideEscapedJSONPointerPath(t *testing.T) {
	body := []byte(`{"metadata":{"a/b":{"old":true}}}`)
	override := `[{"op":"replace","path":"/metadata/a~1b/old","value":false}]`
	payload, _, err := ApplyParamOverridePayload(body, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverridePayload() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	metadata := decoded["metadata"].(map[string]any)
	entry := metadata["a/b"].(map[string]any)
	if entry["old"] != false {
		t.Fatalf("escaped pointer payload = %#v", decoded)
	}
}

func TestApplyParamOverrideMoveUsesRemoveThenAddArraySemantics(t *testing.T) {
	body := []byte(`{"items":["a","b","c"]}`)
	override := `[{"op":"move","from":"/items/0","path":"/items/2"}]`
	payload, _, err := ApplyParamOverridePayload(body, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverridePayload() error = %v", err)
	}
	var decoded struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	want := []string{"b", "c", "a"}
	if len(decoded.Items) != len(want) {
		t.Fatalf("items = %#v, want %#v", decoded.Items, want)
	}
	for i := range want {
		if decoded.Items[i] != want[i] {
			t.Fatalf("items = %#v, want %#v", decoded.Items, want)
		}
	}
}

func TestApplyParamOverrideLeavesMultipartBodyUntouched(t *testing.T) {
	body := []byte("--boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nalias\r\n--boundary--\r\n")
	override := `{"temperature":0.2}`
	req, err := http.NewRequest(http.MethodPost, "https://example.com", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	payload, captured, err := ApplyParamOverrideWithPayload(req, &override)
	if err != nil {
		t.Fatalf("ApplyParamOverrideWithPayload() error = %v", err)
	}
	if !captured || !bytes.Equal(payload, body) {
		t.Fatalf("multipart payload changed: captured=%t payload=%q", captured, payload)
	}
	actual, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, body) {
		t.Fatalf("request body = %q, want original %q", actual, body)
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
