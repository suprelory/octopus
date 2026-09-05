package gemini

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/bestruirui/octopus/internal/utils/log"
)

func cleanGeminiSchema(schema map[string]any) {
	if schema == nil {
		return
	}
	t := &geminiSchemaTransformer{
		root:    schema,
		visited: map[uintptr]struct{}{},
	}
	t.transform(schema)
}

type geminiSchemaTransformer struct {
	root    map[string]any
	visited map[uintptr]struct{}
}

func (t *geminiSchemaTransformer) transform(schemaNode any) {
	if schemaNode == nil {
		return
	}

	// Cycle guard: schema graphs can contain shared sub-objects (or be cyclic after merges).
	// We only track reference-like kinds to avoid false positives.
	rv := reflect.ValueOf(schemaNode)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer:
		if rv.IsNil() {
			return
		}
		id := rv.Pointer()
		if _, seen := t.visited[id]; seen {
			return
		}
		t.visited[id] = struct{}{}
	}

	switch node := schemaNode.(type) {
	case []any:
		for _, item := range node {
			t.transform(item)
		}
		return

	case map[string]any:
		// 1) Resolve $ref (local-only: #/...)
		if ref, ok := node["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
			path := strings.Split(ref[2:], "/")
			var cur any = t.root
			for _, seg := range path {
				seg = strings.ReplaceAll(seg, "~1", "/")
				seg = strings.ReplaceAll(seg, "~0", "~")
				m, ok := cur.(map[string]any)
				if !ok {
					cur = nil
					break
				}
				cur = m[seg]
				if cur == nil {
					break
				}
			}

			if resolved, ok := cur.(map[string]any); ok && resolved != nil {
				// Merge resolved schema into node, but keep local overrides in node.
				overlay := make(map[string]any, len(node))
				for k, v := range node {
					if k != "$ref" {
						overlay[k] = v
					}
				}

				var copied map[string]any
				if b, err := json.Marshal(resolved); err == nil {
					if err := json.Unmarshal(b, &copied); err != nil {
						log.Warnf("gemini: failed to deep-copy resolved schema: %v", err)
					}
				}
				if copied == nil {
					copied = make(map[string]any, len(resolved))
					for k, v := range resolved {
						copied[k] = v
					}
				}

				for k := range node {
					delete(node, k)
				}
				for k, v := range copied {
					node[k] = v
				}
				for k, v := range overlay {
					node[k] = v
				}
				delete(node, "$ref")
			}
		}

		// 2) Merge allOf into current node
		if allOf, ok := node["allOf"].([]any); ok {
			for _, item := range allOf {
				t.transform(item)
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}

				// Merge properties (existing props win)
				if itemProps, ok := itemMap["properties"].(map[string]any); ok {
					props, _ := node["properties"].(map[string]any)
					if props == nil {
						props = map[string]any{}
					}
					for k, v := range itemProps {
						if _, exists := props[k]; !exists {
							props[k] = v
						}
					}
					node["properties"] = props
				}

				// Merge required
				itemReq := t.asStringSlice(itemMap["required"])
				if len(itemReq) > 0 {
					curReq := t.asStringSlice(node["required"])
					curReq = append(curReq, itemReq...)
					node["required"] = t.dedupeStrings(curReq)
				}
			}
			delete(node, "allOf")
		}

		// 3) Type mapping (and nullable union handling)
		if typ, ok := node["type"]; ok {
			primary := ""
			switch v := typ.(type) {
			case string:
				primary = v
			case []any:
				for _, it := range v {
					if s, ok := it.(string); ok && strings.ToLower(s) != "null" {
						primary = s
						break
					}
				}
			case []string:
				for _, s := range v {
					if strings.ToLower(s) != "null" {
						primary = s
						break
					}
				}
			}

			switch strings.ToLower(primary) {
			case "string":
				node["type"] = "STRING"
			case "number":
				node["type"] = "NUMBER"
			case "integer":
				node["type"] = "INTEGER"
			case "boolean":
				node["type"] = "BOOLEAN"
			case "array":
				node["type"] = "ARRAY"
			case "object":
				node["type"] = "OBJECT"
			}
		}

		// 4) ARRAY items fixes + tuple handling
		if node["type"] == "ARRAY" {
			if node["items"] == nil {
				node["items"] = map[string]any{}
			} else if tuple, ok := node["items"].([]any); ok {
				for _, it := range tuple {
					t.transform(it)
				}

				// Add tuple hint to description
				tupleTypes := make([]string, 0, len(tuple))
				for _, it := range tuple {
					if itMap, ok := it.(map[string]any); ok {
						if tt, ok := itMap["type"].(string); ok && tt != "" {
							tupleTypes = append(tupleTypes, tt)
						} else {
							tupleTypes = append(tupleTypes, "any")
						}
					} else {
						tupleTypes = append(tupleTypes, "any")
					}
				}
				hint := fmt.Sprintf("(Tuple: [%s])", strings.Join(tupleTypes, ", "))
				if origDesc, _ := node["description"].(string); origDesc == "" {
					node["description"] = hint
				} else {
					node["description"] = strings.TrimSpace(origDesc + " " + hint)
				}

				// Homogeneous tuple => collapse to list schema; otherwise loosen.
				firstType := ""
				if len(tuple) > 0 {
					if itMap, ok := tuple[0].(map[string]any); ok {
						firstType, _ = itMap["type"].(string)
					}
				}
				isHomogeneous := firstType != ""
				for _, it := range tuple {
					itMap, ok := it.(map[string]any)
					if !ok {
						isHomogeneous = false
						break
					}
					tt, _ := itMap["type"].(string)
					if tt != firstType {
						isHomogeneous = false
						break
					}
				}

				if isHomogeneous {
					node["items"] = tuple[0]
				} else {
					node["items"] = map[string]any{}
				}
			}
		}

		// 5) anyOf: try const->enum; otherwise take first usable schema if no type set
		if anyOf, ok := node["anyOf"].([]any); ok {
			for _, item := range anyOf {
				t.transform(item)
			}

			allConst := true
			enumVals := make([]string, 0, len(anyOf))
			for _, item := range anyOf {
				itemMap, ok := item.(map[string]any)
				if !ok {
					allConst = false
					break
				}
				c, ok := itemMap["const"]
				if !ok {
					allConst = false
					break
				}
				if c == nil || c == "" {
					continue
				}
				enumVals = append(enumVals, fmt.Sprint(c))
			}

			if allConst && len(enumVals) > 0 {
				node["type"] = "STRING"
				node["enum"] = enumVals
			} else if _, hasType := node["type"]; !hasType {
				for _, item := range anyOf {
					if itemMap, ok := item.(map[string]any); ok {
						if itemMap["type"] != nil || itemMap["enum"] != nil {
							for k, v := range itemMap {
								node[k] = v
							}
							break
						}
					}
				}
			}
			delete(node, "anyOf")
		}

		// 6) Default value -> description hint (then delete default)
		if def, ok := node["default"]; ok {
			if desc, ok := node["description"].(string); ok && desc != "" {
				if b, err := json.Marshal(def); err == nil {
					node["description"] = desc + " (Default: " + string(b) + ")"
				}
			}
		}

		// 7) Remove unsupported fields
		for _, k := range []string{
			"title", "$schema", "$ref", "strict",
			"exclusiveMaximum", "exclusiveMinimum",
			"additionalProperties", "oneOf", "default",
			"propertyNames",
			"$defs",
			"uniqueItems", "multipleOf",
		} {
			delete(node, k)
		}

		// 8) Recurse into properties/items
		if props, ok := node["properties"].(map[string]any); ok {
			for _, prop := range props {
				t.transform(prop)
			}
		}
		if items := node["items"]; items != nil {
			t.transform(items)
		}

		// 9) Ensure required is de-duped (allOf merge can introduce duplicates)
		if req := t.asStringSlice(node["required"]); len(req) > 0 {
			node["required"] = t.dedupeStrings(req)
		}
	}
}

func (t *geminiSchemaTransformer) asStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, it := range s {
			if str, ok := it.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func (t *geminiSchemaTransformer) dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
