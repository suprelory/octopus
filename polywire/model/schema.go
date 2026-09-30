package model

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrSchemaLossy is returned by schema-to-provider converters when the input
// contains JSON Schema keywords that the target provider cannot express. The
// caller (relay layer) may choose to route the request to a more capable
// provider, degrade gracefully (drop the schema), or surface the error to
// the client.
var ErrSchemaLossy = errors.New("schema: lossy conversion for target provider")

// Schema is the internal representation of a structured-output schema. It
// captures the subset of JSON Schema Draft-07 that all three providers
// (OpenAI / Anthropic / Gemini) can express between them. Unsupported
// keywords are either dropped (with ErrSchemaLossy) or tunnelled through the
// associated RawSchema field on ResponseFormat when the caller opts to
// passthrough.
//
// Pointer-typed numeric fields use *float64 / *int64 so the zero value is
// distinguishable from "unset", which matters for minimum/maximum/minItems.
type Schema struct {
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Title                string             `json:"title,omitempty"`
	Nullable             bool               `json:"nullable,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *AdditionalProps   `json:"additionalProperties,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	AllOf                []*Schema          `json:"allOf,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	MinItems             *int64             `json:"minItems,omitempty"`
	MaxItems             *int64             `json:"maxItems,omitempty"`
	MinLength            *int64             `json:"minLength,omitempty"`
	MaxLength            *int64             `json:"maxLength,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	PropertyOrdering     []string           `json:"propertyOrdering,omitempty"`
	Default              any                `json:"default,omitempty"`
	Const                any                `json:"const,omitempty"`
	Defs                 map[string]*Schema `json:"$defs,omitempty"`
	Definitions          map[string]*Schema `json:"definitions,omitempty"`

	// Ref is the JSON Schema $ref target. Gemini conversion resolves local
	// references and reports unresolved or recursive references as lossy.
	Ref string `json:"$ref,omitempty"`
}

// AdditionalProps models the JSON Schema `additionalProperties` keyword,
// which can be either a boolean or a schema. Gemini rejects both forms;
// OpenAI Responses requires additionalProperties:false on strict objects.
type AdditionalProps struct {
	// Bool reflects the boolean form. Nil means "use Schema instead".
	Bool *bool
	// Schema is the nested-schema form.
	Schema *Schema
}

// MarshalJSON preserves the two legal wire forms — bool or schema.
func (a AdditionalProps) MarshalJSON() ([]byte, error) {
	if a.Bool != nil {
		return json.Marshal(*a.Bool)
	}
	if a.Schema != nil {
		return json.Marshal(a.Schema)
	}
	return []byte("null"), nil
}

// UnmarshalJSON accepts either a bool or an object.
func (a *AdditionalProps) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		a.Bool = &b
		return nil
	}
	var s Schema
	if err := json.Unmarshal(data, &s); err == nil {
		a.Schema = &s
		return nil
	}
	return fmt.Errorf("schema: additionalProperties must be bool or object")
}

// ParseSchema decodes a JSON Schema into the internal Schema type. It
// accepts both the OpenAI Responses wire shape and bare Draft-07 schemas.
func ParseSchema(raw []byte) (*Schema, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ToGemini converts the Schema into a GeminiSchema suitable for Gemini's
// responseSchema or function-calling parameters. Unsupported keywords
// produce ErrSchemaLossy; the caller must decide whether to drop the
// schema, route to a different provider, or surface the error.
func (s *Schema) ToGemini() (*GeminiSchema, error) {
	if s == nil {
		return nil, nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("schema: encode typed schema: %w", err)
	}
	return ConvertJSONSchemaToGemini(raw)
}

// ToOpenAIResponseFormat emits the OpenAI Responses-API-compatible shape
// `{name, description, strict, schema}`. OpenAI accepts the full Draft-07
// subset so no keywords are dropped; name/description/strict are carried on
// ResponseFormat.JSONSchemaSpec when the client provided them.
//
// The returned bytes are the JSON-encoded schema object ready to slot into
// the OpenAI `response_format.json_schema.schema` field.
func (s *Schema) ToOpenAIResponseFormat() (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// ToAnthropicToolInputSchema emits the schema for use as an Anthropic
// `tool.input_schema`. Anthropic accepts standard Draft-07 so this is
// effectively a re-serialisation, but we normalise `$ref` / `additionalProperties`
// handling because Anthropic's validator is stricter than OpenAI's on those.
func (s *Schema) ToAnthropicToolInputSchema() (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}
