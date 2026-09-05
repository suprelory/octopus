package model

import (
	"encoding/json"
)

// ResponseFormat specifies the format of the response.
//
// OpenAI's `response_format: { type: "json_schema", json_schema: {...} }`
// payload has two parts — a wrapper (name / strict / description) and the
// actual schema. Schema holds the parsed form; RawSchema keeps the original
// bytes so providers that prefer passthrough (or un-parseable schemas) stay
// faithful to the client's intent. Callers should prefer Schema where
// possible and fall back to RawSchema as an escape hatch.
type ResponseFormat struct {
	// Any of "json_schema", "json_object", "text".
	Type string `json:"type"`

	// Name is the OpenAI json_schema.name field (required by the strict
	// OpenAI schema), carried through so outbound emitters can reproduce
	// the wrapper verbatim.
	Name string `json:"name,omitempty"`

	// Description mirrors json_schema.description.
	Description string `json:"description,omitempty"`

	// Strict mirrors json_schema.strict (OpenAI-only; null → client did
	// not specify).
	Strict *bool `json:"strict,omitempty"`

	// Schema is the parsed structured-output schema.
	Schema *Schema `json:"-"`

	// RawSchema preserves the original schema bytes for passthrough /
	// provider-specific forwarding when the typed Schema cannot capture
	// every keyword. Emitters should emit RawSchema only when Schema is
	// nil or when an explicit passthrough is requested.
	RawSchema json.RawMessage `json:"-"`

	// JSONSchema is the legacy field name kept for backward compatibility
	// with code paths that stored the raw schema bytes directly. New code
	// should populate Schema / RawSchema instead.
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

// UnmarshalJSON parses both the canonical OpenAI Responses wire shape
// (`{type, json_schema:{name,strict,schema}}`) and the legacy shape where
// `json_schema` was a bare schema object. It populates Schema/RawSchema
// uniformly so callers can treat them as the authoritative fields.
func (r *ResponseFormat) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type       string          `json:"type"`
		JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Type = raw.Type
	r.JSONSchema = raw.JSONSchema

	if len(raw.JSONSchema) == 0 {
		return nil
	}

	// First attempt: the OpenAI wrapper {name, strict, description, schema}.
	var wrapper struct {
		Name        string          `json:"name,omitempty"`
		Description string          `json:"description,omitempty"`
		Strict      *bool           `json:"strict,omitempty"`
		Schema      json.RawMessage `json:"schema,omitempty"`
	}
	if err := json.Unmarshal(raw.JSONSchema, &wrapper); err == nil && len(wrapper.Schema) > 0 {
		r.Name = wrapper.Name
		r.Description = wrapper.Description
		r.Strict = wrapper.Strict
		r.RawSchema = wrapper.Schema
		if parsed, err := ParseSchema(wrapper.Schema); err == nil {
			r.Schema = parsed
		}
		return nil
	}

	// Fallback: bare schema object (`json_schema: {type:"object", ...}`).
	r.RawSchema = raw.JSONSchema
	if parsed, err := ParseSchema(raw.JSONSchema); err == nil {
		r.Schema = parsed
	}
	return nil
}

// MarshalJSON re-serialises the canonical OpenAI Responses wire shape. When
// Schema is populated it's preferred over RawSchema; JSONSchema is kept as
// the outermost carrier so downstream code that only reads the legacy field
// still works.
func (r ResponseFormat) MarshalJSON() ([]byte, error) {
	type out struct {
		Type       string          `json:"type"`
		JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	}
	o := out{Type: r.Type}

	schemaBytes := r.RawSchema
	if len(schemaBytes) == 0 && r.Schema != nil {
		if b, err := json.Marshal(r.Schema); err == nil {
			schemaBytes = b
		}
	}

	// If we have no schema content but a legacy JSONSchema blob, pass it
	// through unchanged.
	if len(schemaBytes) == 0 {
		o.JSONSchema = r.JSONSchema
		return json.Marshal(o)
	}

	// Re-wrap into {name, strict, description, schema} whenever any of
	// those metadata fields are set; otherwise emit the bare schema for
	// backward compatibility with callers that read json_schema directly.
	if r.Name != "" || r.Description != "" || r.Strict != nil {
		wrapper := struct {
			Name        string          `json:"name,omitempty"`
			Description string          `json:"description,omitempty"`
			Strict      *bool           `json:"strict,omitempty"`
			Schema      json.RawMessage `json:"schema,omitempty"`
		}{
			Name:        r.Name,
			Description: r.Description,
			Strict:      r.Strict,
			Schema:      schemaBytes,
		}
		b, err := json.Marshal(wrapper)
		if err != nil {
			return nil, err
		}
		o.JSONSchema = b
	} else {
		o.JSONSchema = schemaBytes
	}
	return json.Marshal(o)
}
