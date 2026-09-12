package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type ConversionMode string

const (
	ConversionLosslessCanonical ConversionMode = "lossless_canonical"
	ConversionRawSidecar        ConversionMode = "raw_sidecar"
	ConversionLossyCanonical    ConversionMode = "lossy_canonical"
)

// RequestConversion is bounded audit evidence; it never contains raw payloads.
type RequestConversion struct {
	Mode              ConversionMode `json:"mode"`
	ReplayAvailable   bool           `json:"replay_available"`
	RawInputPreserved bool           `json:"raw_input_preserved"`
	ExactReplay       bool           `json:"exact_replay"`
}

// RequestRecovery preserves unknown top-level provider fields for native
// recovery. Other protocols omit these fields and report the loss so routing
// can prefer a preserving channel. RequiredFields survives sidecar removal so
// missing native data still fails closed during replay.
type RequestRecovery struct {
	Format         APIFormat                  `json:"format"`
	RequiredFields []string                   `json:"required_fields,omitempty"`
	Fields         map[string]json.RawMessage `json:"fields,omitempty"`
}

func (r *InternalLLMRequest) CaptureRequestRecovery(body []byte, wire any) error {
	if r == nil || r.Operation == nil {
		return fmt.Errorf("request operation is required for recovery")
	}
	fields, err := UnknownWireFields(body, wire)
	if err != nil {
		return err
	}
	if r.RawAPIFormat == APIFormatOpenAIResponse {
		var eventType string
		if json.Unmarshal(fields["type"], &eventType) == nil && eventType == "response.create" {
			delete(fields, "type")
		}
	}
	if len(fields) == 0 {
		return nil
	}
	recovery := &RequestRecovery{Format: r.RawAPIFormat, Fields: fields}
	for field := range fields {
		recovery.RequiredFields = append(recovery.RequiredFields, field)
	}
	sort.Strings(recovery.RequiredFields)
	r.Operation.Recovery = recovery
	return nil
}

// UnknownWireFields extracts extensions against the adapter's wire schema.
func UnknownWireFields(body []byte, wire any) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	typ := reflect.TypeOf(wire)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("request wire schema must be a struct")
	}
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			delete(fields, name)
		}
	}
	return fields, nil
}

func (r *InternalLLMRequest) ValidateNativeRecovery(target APIFormat, rawPassthrough bool) error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	if rawPassthrough && target == r.RawAPIFormat {
		return nil
	}
	if r.Operation != nil && r.Operation.Recovery != nil {
		recovery := r.Operation.Recovery
		// Unknown top-level fields are required only when the target can
		// actually replay their source protocol. A different protocol has no
		// schema-level recovery path and deliberately drops these opaque fields;
		// known native semantics below remain hard requirements.
		if recovery.Format == target {
			for _, field := range recovery.RequiredFields {
				if !json.Valid(recovery.Fields[field]) {
					return fmt.Errorf("required native field %q has no valid raw sidecar", field)
				}
			}
		}
	}
	if r.RawAPIFormat == APIFormatAnthropicMessage {
		ext := r.GetAnthropicExtensions()
		for _, native := range []struct {
			field string
			raw   json.RawMessage
		}{{"mcp_servers", ext.MCPServers}, {"container", ext.Container}} {
			if r.FieldPresenceOf(native.field) != FieldPresent && len(native.raw) == 0 {
				continue
			}
			if target != APIFormatAnthropicMessage || !json.Valid(native.raw) {
				return fmt.Errorf("required Anthropic %s has no native recovery path", native.field)
			}
		}
		for _, tool := range r.Tools {
			if tool.Type == "function" || tool.Type == "" {
				continue
			}
			if target != APIFormatAnthropicMessage || !json.Valid(tool.AnthropicServerSpec) {
				return fmt.Errorf("required Anthropic tool %q has no raw server-tool spec", tool.Type)
			}
		}
	}
	if !r.HasOpenAIResponsesPassthrough() {
		return nil
	}
	if target != APIFormatOpenAIResponse {
		return fmt.Errorf("native Responses semantics require an OpenAI Responses recovery path: %s", r.OpenAIResponsesPassthroughReasonTextValue())
	}
	for _, reason := range strings.Split(r.OpenAIResponsesPassthroughReasonTextValue(), ",") {
		kind, name, ok := strings.Cut(strings.TrimSpace(reason), ":")
		if !ok || name == "" {
			return fmt.Errorf("native Responses semantic %q has no recovery path", reason)
		}
		var raw json.RawMessage
		switch kind {
		case "input", "input_field":
			raw = r.OpenAIRawInputItems()
		case "tool", "tool_field":
			raw = r.GetOpenAIResponsesOptions().RawTools
		default:
			return fmt.Errorf("native Responses semantic %q has no recovery path", reason)
		}
		field := "type"
		if strings.HasSuffix(kind, "_field") {
			field = name
			name = ""
		}
		if !rawItemsContain(raw, field, name) {
			return fmt.Errorf("required native Responses %s %q has no matching raw sidecar", kind, name)
		}
	}
	return nil
}

// RequestRecoveryChanges reports only the unknown top-level fields omitted by
// MarshalRequestWithRecovery. Native tools and input items have separate
// recovery requirements and are never covered by this fallback.
func (r *InternalLLMRequest) RequestRecoveryChanges(target APIFormat) []RequestTransformationChange {
	if r == nil || r.Operation == nil || r.Operation.Recovery == nil {
		return nil
	}
	recovery := r.Operation.Recovery
	if recovery.Format == target {
		return nil
	}
	fieldSet := make(map[string]struct{}, len(recovery.Fields)+len(recovery.RequiredFields))
	for field := range recovery.Fields {
		fieldSet[field] = struct{}{}
	}
	for _, field := range recovery.RequiredFields {
		fieldSet[field] = struct{}{}
	}
	changes := make([]RequestTransformationChange, 0, len(fieldSet))
	for field := range fieldSet {
		if strings.TrimSpace(field) == "" {
			continue
		}
		changes = append(changes, RequestTransformationChange{
			Field:                field,
			Action:               RequestTransformationDrop,
			Condition:            "target protocol has no recovery path for the unknown top-level field",
			Reason:               fmt.Sprintf("unknown top-level field %q from %s is dropped when converting to %s", field, recovery.Format, target),
			UnknownTopLevelField: true,
		})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
	return changes
}

func rawItemsContain(raw json.RawMessage, field, name string) bool {
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, item := range items {
		var typ string
		if (name == "" && item[field] != nil) || (json.Unmarshal(item[field], &typ) == nil && typ == name) {
			return true
		}
		for _, child := range []string{"content", "output"} {
			if rawItemsContain(item[child], field, name) {
				return true
			}
		}
	}
	return false
}

// MarshalRequestWithRecovery is shared by canonical wire builders and replay.
// Unknown native fields cannot override a builder-owned field.
func MarshalRequestWithRecovery(req *InternalLLMRequest, target APIFormat, wire any) ([]byte, error) {
	if err := req.ValidateNativeRecovery(target, false); err != nil {
		return nil, err
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if req.Operation == nil || req.Operation.Recovery == nil || req.Operation.Recovery.Format != target || len(req.Operation.Recovery.Fields) == 0 {
		return body, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	reserved := make(map[string]bool)
	typ := reflect.TypeOf(wire)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			field := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if field != "" && field != "-" {
				reserved[field] = true
			}
		}
	}
	for field, raw := range req.Operation.Recovery.Fields {
		if _, exists := fields[field]; exists || reserved[field] {
			return nil, fmt.Errorf("native recovery field %q conflicts with a canonical field", field)
		}
		fields[field] = raw
	}
	return json.Marshal(fields)
}

func (r *InternalLLMRequest) ConversionState(target APIFormat, passthrough, lossy bool) RequestConversion {
	state := RequestConversion{Mode: ConversionLosslessCanonical}
	if r == nil {
		return state
	}
	hasSidecar := passthrough
	if target == APIFormatOpenAIResponse {
		var input []json.RawMessage
		state.RawInputPreserved = json.Unmarshal(r.OpenAIRawInputItems(), &input) == nil && input != nil
		hasSidecar = hasSidecar || state.RawInputPreserved || len(r.GetOpenAIResponsesOptions().RawTools) > 0
	}
	if r.Operation != nil && r.Operation.Recovery != nil && r.Operation.Recovery.Format == target {
		hasSidecar = hasSidecar || len(r.Operation.Recovery.Fields) > 0
	}
	if target == APIFormatAnthropicMessage {
		ext := r.GetAnthropicExtensions()
		hasSidecar = hasSidecar || len(ext.MCPServers) > 0 || len(ext.Container) > 0
		for _, tool := range r.Tools {
			hasSidecar = hasSidecar || len(tool.AnthropicServerSpec) > 0
		}
	}
	state.RawInputPreserved = state.RawInputPreserved || passthrough
	state.ReplayAvailable = state.RawInputPreserved || len(r.ConversationMessages()) > 0
	state.ExactReplay = r.IsOpenAIExactReplayRequest() && state.RawInputPreserved && !lossy
	if hasSidecar {
		state.Mode = ConversionRawSidecar
	}
	if lossy {
		state.Mode = ConversionLossyCanonical
		state.ReplayAvailable = false
	}
	return state
}
