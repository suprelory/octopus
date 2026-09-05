package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type FieldPresence uint8

const (
	FieldAbsent FieldPresence = iota
	FieldPresent
	FieldExplicitNull
)

func (r *InternalLLMRequest) SetFieldPresence(field string, presence FieldPresence) {
	if r == nil {
		return
	}
	field = strings.TrimSpace(field)
	if field == "" {
		return
	}
	if presence == FieldAbsent {
		delete(r.Presence, field)
		return
	}
	if r.Presence == nil {
		r.Presence = make(map[string]FieldPresence)
	}
	r.Presence[field] = presence
}

func (r *InternalLLMRequest) FieldPresenceOf(field string) FieldPresence {
	if r == nil || r.Presence == nil {
		return FieldAbsent
	}
	return r.Presence[field]
}

// CaptureFieldPresence records top-level fields exactly as supplied on the
// inbound wire. Empty strings, arrays, and objects are present; JSON null is a
// distinct state. The decoded IR continues to carry typed canonical values.
func (r *InternalLLMRequest) CaptureFieldPresence(body []byte) error {
	if r == nil {
		return errors.New("request is nil")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	r.Presence = make(map[string]FieldPresence, len(fields))
	for field, raw := range fields {
		presence := FieldPresent
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			presence = FieldExplicitNull
		}
		r.Presence[field] = presence
	}
	return nil
}
