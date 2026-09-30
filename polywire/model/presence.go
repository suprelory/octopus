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
		delete(r.EmptyFields, field)
		return
	}
	if r.Presence == nil {
		r.Presence = make(map[string]FieldPresence)
	}
	r.Presence[field] = presence
	delete(r.EmptyFields, field)
	if presence == FieldExplicitNull {
		if r.EmptyFields == nil {
			r.EmptyFields = make(map[string]json.RawMessage)
		}
		r.EmptyFields[field] = json.RawMessage("null")
	}
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
	r.EmptyFields = nil
	for field, raw := range fields {
		presence := FieldPresent
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			presence = FieldExplicitNull
		}
		r.Presence[field] = presence
		if empty, ok := explicitEmptyJSON(raw); ok {
			if r.EmptyFields == nil {
				r.EmptyFields = make(map[string]json.RawMessage)
			}
			r.EmptyFields[field] = empty
		}
	}
	return nil
}

func explicitEmptyJSON(raw json.RawMessage) (json.RawMessage, bool) {
	value := bytes.TrimSpace(raw)
	switch string(value) {
	case "null", `""`:
		return bytes.Clone(value), true
	}
	if len(value) >= 2 && len(bytes.TrimSpace(value[1:len(value)-1])) == 0 &&
		((value[0] == '[' && value[len(value)-1] == ']') || (value[0] == '{' && value[len(value)-1] == '}')) {
		return json.RawMessage{value[0], value[len(value)-1]}, true
	}
	return nil, false
}
