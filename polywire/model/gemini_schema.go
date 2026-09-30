package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const (
	geminiSchemaMaxDepth = 64
	geminiSchemaMaxNodes = 4096
)

// ConvertJSONSchemaToGemini is shared by tool parameters and structured output.
// Local references are resolved against the original document. Unsupported
// constraints return a best-effort schema with ErrSchemaLossy and source paths;
// malformed JSON returns an ordinary error. No remote references are fetched.
func ConvertJSONSchemaToGemini(raw json.RawMessage) (*GeminiSchema, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var root any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("schema: invalid JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("schema: expected one JSON value")
	}
	converter := geminiSchemaConverter{root: root, references: make(map[string]bool)}
	result := converter.convert(root, "schema", 0)
	if len(converter.losses) > 0 {
		sort.Strings(converter.losses)
		return result, fmt.Errorf("%w: %s", ErrSchemaLossy, strings.Join(converter.losses, ", "))
	}
	return result, nil
}

// ToGeminiSchema uses the original schema when available so keywords outside
// the typed Schema subset cannot disappear before conversion is audited.
func (r *ResponseFormat) ToGeminiSchema() (*GeminiSchema, error) {
	if r == nil {
		return nil, nil
	}
	if len(r.RawSchema) > 0 {
		return ConvertJSONSchemaToGemini(r.RawSchema)
	}
	if r.Schema != nil {
		return r.Schema.ToGemini()
	}
	if len(r.JSONSchema) > 0 {
		var wrapper struct {
			Schema json.RawMessage `json:"schema"`
		}
		if json.Unmarshal(r.JSONSchema, &wrapper) == nil && len(wrapper.Schema) > 0 {
			return ConvertJSONSchemaToGemini(wrapper.Schema)
		}
		return ConvertJSONSchemaToGemini(r.JSONSchema)
	}
	return nil, nil
}

type geminiSchemaConverter struct {
	root       any
	references map[string]bool
	nodes      int
	losses     []string
	lossSet    map[string]bool
}

func (c *geminiSchemaConverter) loss(path, reason string) {
	loss := path + " (" + reason + ")"
	if c.lossSet == nil {
		c.lossSet = make(map[string]bool)
	}
	if !c.lossSet[loss] {
		c.losses = append(c.losses, loss)
		c.lossSet[loss] = true
	}
}

func (c *geminiSchemaConverter) convert(value any, path string, depth int) *GeminiSchema {
	result := &GeminiSchema{}
	if c.nodes >= geminiSchemaMaxNodes {
		c.loss(path, "schema expansion limit exceeded")
		return result
	}
	c.nodes++
	if depth >= geminiSchemaMaxDepth {
		c.loss(path, "schema expansion limit exceeded")
		return result
	}
	node, ok := value.(map[string]any)
	if !ok {
		if value != true {
			c.loss(path, "schema must be an object or a permissive boolean")
		}
		return result
	}
	if ref, exists := node["$ref"]; exists {
		name, valid := ref.(string)
		target, resolved := c.resolve(name)
		if !valid || !resolved {
			c.loss(path+".$ref", "unresolved or external reference")
		} else if c.references[name] {
			c.loss(path+".$ref", "recursive reference")
		} else if targetNode, ok := target.(map[string]any); ok {
			merged := make(map[string]any, len(targetNode)+len(node))
			for key, value := range targetNode {
				merged[key] = value
			}
			for key, value := range node {
				if key != "$ref" {
					if previous, exists := merged[key]; exists && key != "description" && key != "title" && key != "$defs" && key != "definitions" && !reflect.DeepEqual(previous, value) {
						c.loss(path+"."+key, "$ref sibling overrides a referenced constraint")
					}
					merged[key] = value
				}
			}
			c.references[name] = true
			result = c.convert(merged, path, depth+1)
			delete(c.references, name)
			return result
		} else {
			c.loss(path+".$ref", "reference target is not an object")
		}
	}

	c.scalars(node, result, path)
	if value, exists := node["type"]; exists {
		types := []any{value}
		if union, ok := value.([]any); ok {
			types = union
			if len(union) == 0 {
				c.loss(path+".type", "empty type union")
			}
		}
		seen := make(map[string]bool)
		var alternatives []*GeminiSchema
		for _, value := range types {
			typ, _ := value.(string)
			typ = strings.ToLower(typ)
			switch typ {
			case "null":
				result.Nullable = true
			case "string", "number", "integer", "boolean", "array", "object":
				if !seen[typ] {
					alternatives = append(alternatives, &GeminiSchema{Type: typ})
					seen[typ] = true
				}
			default:
				c.loss(path+".type", "unsupported type")
			}
		}
		switch len(alternatives) {
		case 0:
			if result.Nullable {
				result.Type = "null"
			}
		case 1:
			result.Type = alternatives[0].Type
		default:
			result.AnyOf = alternatives
		}
	}

	if properties, ok := node["properties"].(map[string]any); ok {
		result.Properties = make(map[string]*GeminiSchema, min(len(properties), geminiSchemaMaxNodes-c.nodes))
		for _, name := range sortedSchemaKeys(properties) {
			if c.nodes >= geminiSchemaMaxNodes {
				c.loss(path+".properties", "schema expansion limit exceeded")
				break
			}
			result.Properties[name] = c.convert(properties[name], path+".properties["+fmt.Sprintf("%q", name)+"]", depth+1)
		}
	} else if _, exists := node["properties"]; exists {
		c.loss(path+".properties", "properties must be an object")
	}
	if items, exists := node["items"]; exists {
		if tuple, ok := items.([]any); ok {
			c.loss(path+".items", "tuple positions cannot be preserved")
			result.Items = &GeminiSchema{AnyOf: c.branches(tuple, path+".items", depth)}
		} else {
			result.Items = c.convert(items, path+".items", depth+1)
		}
	} else if result.Type == "array" {
		result.Items = &GeminiSchema{}
	}
	if branches, ok := node["anyOf"].([]any); ok {
		if len(result.AnyOf) > 0 {
			c.loss(path+".type", "type union intersected with anyOf cannot be preserved")
		}
		result.AnyOf = c.branches(branches, path+".anyOf", depth)
	}
	if branches, ok := node["oneOf"].([]any); ok {
		c.loss(path+".oneOf", "exclusive alternatives cannot be preserved")
		if len(result.AnyOf) == 0 {
			result.AnyOf = c.branches(branches, path+".oneOf", depth)
		}
	}
	if branches, ok := node["allOf"].([]any); ok {
		c.loss(path+".allOf", "intersection constraints cannot be fully preserved")
		for _, branch := range c.branches(branches, path+".allOf", depth) {
			if result.Type == "" {
				result.Type = branch.Type
			}
			if len(branch.Properties) > 0 && result.Properties == nil {
				result.Properties = make(map[string]*GeminiSchema)
			}
			for name, property := range branch.Properties {
				if _, exists := result.Properties[name]; !exists {
					result.Properties[name] = property
				}
			}
			result.Required = append(result.Required, branch.Required...)
		}
	}
	if values, ok := node["enum"].([]any); ok {
		if len(values) == 0 {
			c.loss(path+".enum", "empty enum cannot be preserved")
		}
		onlyStringsAndNull := true
		hasNull := false
		nullAllowedByType := result.Nullable || result.Type == "" || result.Type == "null"
		for _, value := range values {
			if text, ok := value.(string); ok {
				result.Enum = append(result.Enum, text)
			} else if value == nil {
				hasNull = true
			} else {
				c.loss(path+".enum", "non-string enum value")
				onlyStringsAndNull = false
			}
		}
		result.Nullable = hasNull && nullAllowedByType
		if onlyStringsAndNull && len(result.Enum) == 0 && hasNull {
			if nullAllowedByType {
				result.Type = "null"
			} else {
				c.loss(path+".enum", "enum excludes every value of the declared type")
			}
		}
		if len(result.Enum) > 0 && result.Type != "" && result.Type != "string" {
			c.loss(path+".enum", "string enum conflicts with the declared type")
		}
		if onlyStringsAndNull && result.Type == "" {
			if len(result.Enum) > 0 {
				result.Type = "string"
			} else if hasNull {
				result.Type = "null"
			}
		}
	} else if _, exists := node["enum"]; exists {
		c.loss(path+".enum", "enum must be an array")
	}
	if value, exists := node["const"]; exists {
		if text, ok := value.(string); ok && (result.Type == "" || result.Type == "string") && len(result.Enum) == 0 {
			result.Type, result.Enum = "string", []string{text}
		} else {
			c.loss(path+".const", "const constraint cannot be preserved")
		}
	}
	if len(result.Enum) > 0 && result.Format == "" && result.Type == "string" {
		result.Format = "enum"
	}
	result.Required = uniqueSchemaStrings(result.Required)
	for key := range node {
		switch key {
		case "$ref", "$defs", "definitions", "$schema", "type", "format", "description", "nullable", "enum", "const",
			"required", "propertyOrdering", "properties", "items", "minimum", "maximum", "minItems", "maxItems":
		case "anyOf", "oneOf", "allOf":
			if _, ok := node[key].([]any); !ok {
				c.loss(path+"."+key, "schema alternatives must be an array")
			}
		case "minLength", "maxLength":
			c.loss(path+"."+key, "min/maxLength is unsupported")
		default:
			c.loss(path+"."+key, "unsupported keyword")
		}
	}
	return result
}

func (c *geminiSchemaConverter) branches(values []any, path string, depth int) []*GeminiSchema {
	if len(values) == 0 {
		c.loss(path, "empty schema alternatives cannot be preserved")
	}
	result := make([]*GeminiSchema, 0, min(len(values), geminiSchemaMaxNodes-c.nodes))
	for index, value := range values {
		if c.nodes >= geminiSchemaMaxNodes {
			c.loss(path, "schema expansion limit exceeded")
			break
		}
		result = append(result, c.convert(value, fmt.Sprintf("%s[%d]", path, index), depth+1))
	}
	return result
}

// Read the supported scalars directly instead of serializing every schema node
// through a temporary JSON object. UseNumber keeps integer bounds exact.
func (c *geminiSchemaConverter) scalars(node map[string]any, result *GeminiSchema, path string) {
	invalid := func(key string) { c.loss(path+"."+key, "invalid schema field value") }
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"format", &result.Format}, {"description", &result.Description},
	} {
		if value, exists := node[field.name]; exists {
			if text, ok := value.(string); ok {
				*field.target = text
			} else {
				invalid(field.name)
			}
		}
	}
	if value, exists := node["nullable"]; exists {
		if nullable, ok := value.(bool); ok {
			result.Nullable = nullable
		} else {
			invalid("nullable")
		}
	}
	for _, field := range []struct {
		name   string
		target *[]string
	}{
		{"required", &result.Required}, {"propertyOrdering", &result.PropertyOrdering},
	} {
		if value, exists := node[field.name]; exists {
			values, ok := value.([]any)
			if !ok {
				invalid(field.name)
				continue
			}
			for _, value := range values {
				if text, ok := value.(string); ok {
					*field.target = append(*field.target, text)
				} else {
					invalid(field.name)
				}
			}
		}
	}
	for _, field := range []struct {
		name   string
		target **float64
	}{
		{"minimum", &result.Minimum}, {"maximum", &result.Maximum},
	} {
		if value, exists := node[field.name]; exists {
			number, ok := value.(json.Number)
			if !ok {
				invalid(field.name)
				continue
			}
			parsed, err := number.Float64()
			if err != nil {
				invalid(field.name)
				continue
			}
			*field.target = &parsed
		}
	}
	for _, field := range []struct {
		name   string
		target **int64
	}{
		{"minItems", &result.MinItems}, {"maxItems", &result.MaxItems},
	} {
		if value, exists := node[field.name]; exists {
			number, ok := value.(json.Number)
			if !ok {
				invalid(field.name)
				continue
			}
			parsed, err := number.Int64()
			if err != nil {
				invalid(field.name)
				continue
			}
			*field.target = &parsed
		}
	}
}

func (c *geminiSchemaConverter) resolve(ref string) (any, bool) {
	if ref == "#" {
		return c.root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	value := c.root
	for _, segment := range strings.Split(ref[2:], "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch node := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = node[segment]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			value = node[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func sortedSchemaKeys(node map[string]any) []string {
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSchemaStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := values[:0]
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
