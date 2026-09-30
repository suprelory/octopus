package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGeminiSchemaPreservesReferencesAndUnions(t *testing.T) {
	raw := json.RawMessage(`{"$defs":{"a/b~c":{"type":["string","null"],"enum":["","yes",null]}},"type":"object","properties":{"first":{"$ref":"#/$defs/a~1b~0c","description":"first"},"second":{"$ref":"#/$defs/a~1b~0c"},"union":{"anyOf":[{"type":"string"},{"type":"integer"}]},"types":{"type":["string","integer","null"]}}}`)
	schema, err := ConvertJSONSchemaToGemini(raw)
	if err != nil {
		t.Fatal(err)
	}
	first, second := schema.Properties["first"], schema.Properties["second"]
	if first.Type != "string" || !first.Nullable || len(first.Enum) != 2 || first.Enum[0] != "" || first.Description != "first" || second.Description != "" {
		t.Fatalf("reference or null/empty enum semantics changed: %+v %+v", first, second)
	}
	first.Enum[0] = "changed"
	if second.Enum[0] != "" {
		t.Fatal("independently resolved references share mutable output")
	}
	if len(schema.Properties["union"].AnyOf) != 2 || len(schema.Properties["types"].AnyOf) != 2 || !schema.Properties["types"].Nullable {
		t.Fatal("union alternatives were dropped")
	}
	encoded, err := json.Marshal(schema)
	if err != nil || strings.Contains(string(encoded), `"type":""`) || strings.Contains(string(encoded), `"$ref"`) {
		t.Fatalf("invalid Gemini wire schema: %s, %v", encoded, err)
	}
}

func TestGeminiSchemaReportsUnrepresentableConstraints(t *testing.T) {
	for _, test := range []struct{ name, raw, loss string }{
		{"recursive", `{"$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},"$ref":"#/$defs/node"}`, "recursive reference"},
		{"root reference", `{"type":"object","properties":{"next":{"$ref":"#"}}}`, "recursive reference"},
		{"external", `{"$ref":"https://example.invalid/schema"}`, "unresolved or external reference"},
		{"unknown", `{"type":"object","properties":{"child":{"type":"object","unevaluatedProperties":false}}}`, `properties["child"].unevaluatedProperties`},
		{"exclusive", `{"oneOf":[{"type":"string"},{"type":"number"}]}`, ".oneOf"},
		{"tuple", `{"type":"array","items":[{"type":"string"},{"type":"number"}]}`, "tuple positions"},
		{"numeric enum", `{"type":"integer","enum":[1,2]}`, "non-string enum"},
		{"conflicting ref", `{"$defs":{"x":{"type":"string"}},"$ref":"#/$defs/x","type":"number"}`, "$ref sibling"},
		{"false schema", `false`, "permissive boolean"},
		{"malformed alternatives", `{"anyOf":{}}`, "alternatives must be an array"},
		{"empty alternatives", `{"anyOf":[]}`, "empty schema alternatives"},
		{"conflicting enum type", `{"type":"integer","enum":["one"]}`, "string enum conflicts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema, err := ConvertJSONSchemaToGemini(json.RawMessage(test.raw))
			if !errors.Is(err, ErrSchemaLossy) || !strings.Contains(err.Error(), test.loss) {
				t.Fatalf("missing conversion loss %q: %v", test.loss, err)
			}
			if _, marshalErr := json.Marshal(schema); marshalErr != nil {
				t.Fatal(marshalErr)
			}
		})
	}
}

func TestGeminiSchemaEnumNullHonorsTypeIntersection(t *testing.T) {
	for _, test := range []struct {
		raw      string
		typ      string
		nullable bool
	}{
		{`{"type":["string","null"],"enum":["a"]}`, "string", false},
		{`{"type":"string","enum":["a",null]}`, "string", false},
		{`{"type":["string","null"],"enum":[null]}`, "null", true},
	} {
		schema, err := ConvertJSONSchemaToGemini(json.RawMessage(test.raw))
		if err != nil || schema.Type != test.typ || schema.Nullable != test.nullable {
			t.Fatalf("enum/type intersection changed for %s: %+v %v", test.raw, schema, err)
		}
	}
}

func TestGeminiSchemaBoundsExpansionAndRejectsMalformedJSON(t *testing.T) {
	raw := strings.Repeat(`{"type":"array","items":`, geminiSchemaMaxDepth+5) + `{"type":"string"}` + strings.Repeat("}", geminiSchemaMaxDepth+5)
	if _, err := ConvertJSONSchemaToGemini(json.RawMessage(raw)); !errors.Is(err, ErrSchemaLossy) || !strings.Contains(err.Error(), "expansion limit") {
		t.Fatalf("unbounded schema depth: %v", err)
	}
	if _, err := ConvertJSONSchemaToGemini(json.RawMessage(`{"type":`)); err == nil || errors.Is(err, ErrSchemaLossy) {
		t.Fatalf("malformed JSON must fail parsing: %v", err)
	}
	if _, err := ConvertJSONSchemaToGemini(json.RawMessage(`{} {}`)); err == nil || errors.Is(err, ErrSchemaLossy) {
		t.Fatalf("multiple JSON values must fail parsing: %v", err)
	}
	cyclic := &Schema{Type: "array"}
	cyclic.Items = cyclic
	if _, err := cyclic.ToGemini(); err == nil {
		t.Fatal("cyclic typed schema must fail safely")
	}
}

func TestGeminiSchemaPreservesIntegerBounds(t *testing.T) {
	schema, err := ConvertJSONSchemaToGemini(json.RawMessage(`{"type":"array","minItems":0,"maxItems":9007199254740993,"items":{"type":"string"}}`))
	if err != nil || schema.MinItems == nil || *schema.MinItems != 0 || schema.MaxItems == nil || *schema.MaxItems != 9007199254740993 {
		t.Fatalf("integer schema bound was rounded: %+v %v", schema, err)
	}
}

func TestGeminiSchemaCountsTruncatedNodesAgainstExpansionLimit(t *testing.T) {
	properties := make(map[string]any, geminiSchemaMaxNodes+1)
	for index := range geminiSchemaMaxNodes + 1 {
		properties[fmt.Sprintf("p%04d", index)] = map[string]any{"type": "string"}
	}
	leaf, err := json.Marshal(map[string]any{"type": "object", "properties": properties})
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Repeat(`{"type":"array","items":`, geminiSchemaMaxDepth-1) + string(leaf) + strings.Repeat("}", geminiSchemaMaxDepth-1)
	schema, err := ConvertJSONSchemaToGemini(json.RawMessage(raw))
	if !errors.Is(err, ErrSchemaLossy) {
		t.Fatalf("missing expansion loss: %v", err)
	}
	for depth := 0; depth < geminiSchemaMaxDepth-1; depth++ {
		schema = schema.Items
	}
	if len(schema.Properties)+geminiSchemaMaxDepth > geminiSchemaMaxNodes {
		t.Fatalf("depth truncation bypassed node budget: %d leaves", len(schema.Properties))
	}
}
