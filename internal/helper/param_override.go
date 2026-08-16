package helper

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// ParamOverrideOperation is a small JSON-Patch-like operation used for
// channel-level request customization. Paths use JSON Pointer syntax.
type ParamOverrideOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	From  string `json:"from,omitempty"`
	Value any    `json:"value,omitempty"`
}

// ParamOverrideInspection is the immutable, request-scoped description of a
// channel override. Relay uses it while ranking candidates so a wire patch
// cannot be mistaken for byte-stable native passthrough.
type ParamOverrideInspection struct {
	Active      bool
	Valid       bool
	Fingerprint string
	Paths       []string
}

// InspectParamOverride parses only the override document. It does not inspect
// or mutate a request body and is therefore safe during candidate ranking.
// Invalid JSON remains inactive for backwards compatibility: the relay has
// historically ignored invalid override syntax.
func InspectParamOverride(paramOverride *string) ParamOverrideInspection {
	if paramOverride == nil {
		return ParamOverrideInspection{}
	}
	rawText := strings.TrimSpace(*paramOverride)
	if rawText == "" {
		return ParamOverrideInspection{}
	}
	sum := sha256.Sum256([]byte(rawText))
	inspection := ParamOverrideInspection{Fingerprint: hex.EncodeToString(sum[:])}
	raw := json.RawMessage(rawText)
	if raw[0] == '[' {
		var operations []ParamOverrideOperation
		if err := json.Unmarshal(raw, &operations); err != nil {
			return inspection
		}
		if !validParamOverrideOperations(operations) {
			return inspection
		}
		inspection.Active = true
		inspection.Valid = true
		if canonical, err := json.Marshal(operations); err == nil {
			sum := sha256.Sum256(canonical)
			inspection.Fingerprint = hex.EncodeToString(sum[:])
		}
		paths := make([]string, 0, len(operations)*2)
		for _, operation := range operations {
			if path := strings.TrimSpace(operation.Path); path != "" {
				paths = append(paths, path)
			}
			if from := strings.TrimSpace(operation.From); from != "" {
				paths = append(paths, from)
			}
		}
		inspection.Paths = uniqueSortedStrings(paths)
		if len(operations) == 0 {
			inspection.Active = false
		}
		return inspection
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return inspection
	}
	inspection.Active = true
	inspection.Valid = true
	if canonicalValue, err := json.Marshal(object); err == nil {
		sum := sha256.Sum256(canonicalValue)
		inspection.Fingerprint = hex.EncodeToString(sum[:])
	}
	paths := make([]string, 0, len(object))
	for key := range object {
		paths = append(paths, "/"+escapeJSONPointerToken(key))
	}
	inspection.Paths = uniqueSortedStrings(paths)
	if len(object) == 0 {
		inspection.Active = false
	}
	return inspection
}

func validParamOverrideOperations(operations []ParamOverrideOperation) bool {
	for _, operation := range operations {
		op := strings.ToLower(strings.TrimSpace(operation.Op))
		path := strings.TrimSpace(operation.Path)
		switch op {
		case "add", "replace":
			if path != "" {
				if _, err := pointerTokens(path); err != nil {
					return false
				}
			}
		case "set":
			if path == "" {
				return false
			}
			if _, err := pointerTokens(path); err != nil {
				return false
			}
		case "remove", "delete":
			if path == "" {
				return false
			}
			if _, err := pointerTokens(path); err != nil {
				return false
			}
		case "copy", "move":
			from := strings.TrimSpace(operation.From)
			if path == "" || from == "" {
				return false
			}
			if _, err := pointerTokens(path); err != nil {
				return false
			}
			if _, err := pointerTokens(from); err != nil {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func escapeJSONPointerToken(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if value == "" || (len(result) > 0 && result[len(result)-1] == value) {
			continue
		}
		result = append(result, value)
	}
	return result
}

// ApplyParamOverride accepts both the legacy JSON object merge and the new
// operation-array form. Invalid override syntax is ignored for compatibility;
// valid operations return an error when they cannot be applied safely.
func ApplyParamOverride(request *http.Request, paramOverride *string) error {
	_, _, err := ApplyParamOverrideWithPayload(request, paramOverride)
	return err
}

// ApplyParamOverridePayload applies the same override semantics as
// ApplyParamOverrideWithPayload to an already buffered payload. It is used by
// transports such as WebSocket that do not have an *http.Request to mutate.
func ApplyParamOverridePayload(body []byte, paramOverride *string) (payload []byte, captured bool, err error) {
	if paramOverride == nil || strings.TrimSpace(*paramOverride) == "" {
		return body, false, nil
	}

	raw := json.RawMessage(strings.TrimSpace(*paramOverride))
	if len(raw) == 0 {
		return body, false, nil
	}
	inspection := InspectParamOverride(paramOverride)
	if inspection.Valid && !inspection.Active {
		// Empty object/operation documents are valid configuration, but they
		// must not re-marshal an otherwise byte-stable request.
		return body, false, nil
	}
	// Overrides are JSON body operations. Never reinterpret multipart, binary,
	// or malformed payloads as an empty JSON document: doing so would silently
	// discard the original request body while retaining its Content-Type.
	if !json.Valid(body) {
		return body, true, nil
	}
	if raw[0] == '[' {
		var operations []ParamOverrideOperation
		if err := json.Unmarshal(raw, &operations); err != nil {
			return body, true, nil
		}
		var document any
		if err := json.Unmarshal(body, &document); err != nil {
			return body, true, nil
		}
		for _, operation := range operations {
			if err := applyParamOperation(&document, operation); err != nil {
				return body, true, err
			}
		}
		modified, err := json.Marshal(document)
		if err != nil {
			return body, true, fmt.Errorf("failed to marshal request body with param operations: %w", err)
		}
		return modified, true, nil
	}

	var override map[string]any
	if err := json.Unmarshal(raw, &override); err != nil {
		return body, true, nil
	}
	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return body, true, nil
	}
	if bodyMap == nil {
		return body, true, nil
	}
	for key, value := range override {
		bodyMap[key] = value
	}
	modified, err := json.Marshal(bodyMap)
	if err != nil {
		return body, true, fmt.Errorf("failed to marshal request body with param override: %w", err)
	}
	return modified, true, nil
}

// ApplyParamOverrideWithPayload applies the configured override and returns the
// request payload already read while doing so. captured is false when no
// override work was needed and the request body was left untouched. The
// returned payload also backs request.Body and must be treated as read-only.
func ApplyParamOverrideWithPayload(request *http.Request, paramOverride *string) (payload []byte, captured bool, err error) {
	if request == nil || request.Body == nil || paramOverride == nil || strings.TrimSpace(*paramOverride) == "" {
		return nil, false, nil
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read request body: %w", err)
	}
	restoreBody := func(payload []byte) {
		request.Body = io.NopCloser(bytes.NewReader(payload))
		request.ContentLength = int64(len(payload))
		request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	}
	restoreBody(body)
	if !paramOverrideBodySupported(request, body) {
		return body, true, nil
	}

	modified, captured, err := ApplyParamOverridePayload(body, paramOverride)
	if err != nil {
		return body, captured, err
	}
	restoreBody(modified)
	return modified, captured, nil
}

// paramOverrideBodySupported limits wire patches to JSON requests. A missing
// Content-Type is allowed for compatibility with callers that construct an
// outbound request before setting headers; the payload still must be valid JSON.
func paramOverrideBodySupported(request *http.Request, body []byte) bool {
	if request == nil || len(body) == 0 || !json.Valid(body) {
		return false
	}
	contentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || mediaType == "text/json" || strings.HasSuffix(mediaType, "+json")
}

func applyParamOperation(document *any, operation ParamOverrideOperation) error {
	op := strings.ToLower(strings.TrimSpace(operation.Op))
	path := strings.TrimSpace(operation.Path)
	from := strings.TrimSpace(operation.From)
	if path == "" && op != "replace" && op != "add" {
		return fmt.Errorf("param override %s requires a path", operation.Op)
	}
	switch op {
	case "add", "replace", "set":
		return pointerSet(document, path, operation.Value, op == "add")
	case "remove", "delete":
		return pointerRemove(document, path)
	case "copy", "move":
		value, err := pointerGet(*document, from)
		if err != nil {
			return err
		}
		value, err = cloneJSONValue(value)
		if err != nil {
			return fmt.Errorf("param override %s value: %w", op, err)
		}
		// JSON Patch evaluates a move as remove-then-add. Inserting first is
		// incorrect for array indices because the source index shifts.
		if op == "move" {
			if err := pointerRemove(document, from); err != nil {
				return err
			}
		}
		return pointerSet(document, path, value, true)
	default:
		return fmt.Errorf("unsupported param override operation %q", operation.Op)
	}
}

func cloneJSONValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var cloned any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func pointerTokens(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("param override path must use JSON Pointer syntax")
	}
	parts := strings.Split(path[1:], "/")
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] != '~' {
				continue
			}
			if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
				return nil, fmt.Errorf("param override path contains an invalid JSON Pointer escape")
			}
			j++
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func pointerPath(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	escaped := make([]string, len(tokens))
	for i, token := range tokens {
		escaped[i] = escapeJSONPointerToken(token)
	}
	return "/" + strings.Join(escaped, "/")
}

func pointerGet(document any, path string) (any, error) {
	tokens, err := pointerTokens(path)
	if err != nil {
		return nil, err
	}
	current := document
	for _, token := range tokens {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[token]
			if !ok {
				return nil, fmt.Errorf("param override path %q not found", path)
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(value) {
				return nil, fmt.Errorf("param override array index %q is invalid", token)
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("param override path %q traverses a scalar", path)
		}
	}
	return current, nil
}

func pointerSet(document *any, path string, value any, allowAppend bool) error {
	tokens, err := pointerTokens(path)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		*document = value
		return nil
	}
	var parent any
	if len(tokens) == 1 {
		parent = *document
	} else {
		parent, err = pointerGet(*document, pointerPath(tokens[:len(tokens)-1]))
		if err != nil {
			return err
		}
	}
	last := tokens[len(tokens)-1]
	switch target := parent.(type) {
	case map[string]any:
		target[last] = value
	case []any:
		if allowAppend {
			index, indexErr := strconv.Atoi(last)
			if last == "-" {
				index = len(target)
			} else if indexErr != nil || index < 0 || index > len(target) {
				return fmt.Errorf("param override array index %q is invalid", last)
			}
			updated := make([]any, 0, len(target)+1)
			updated = append(updated, target[:index]...)
			updated = append(updated, value)
			updated = append(updated, target[index:]...)
			parentPath := ""
			if len(tokens) > 1 {
				parentPath = pointerPath(tokens[:len(tokens)-1])
			}
			return pointerSet(document, parentPath, updated, false)
		}
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(target) {
			return fmt.Errorf("param override array index %q is invalid", last)
		}
		target[index] = value
	default:
		return fmt.Errorf("param override path %q parent is not mutable", path)
	}
	return nil
}

func pointerRemove(document *any, path string) error {
	tokens, err := pointerTokens(path)
	if err != nil || len(tokens) == 0 {
		return fmt.Errorf("param override remove requires a non-root path")
	}
	var parent any
	if len(tokens) == 1 {
		parent = *document
	} else {
		parent, err = pointerGet(*document, pointerPath(tokens[:len(tokens)-1]))
		if err != nil {
			return err
		}
	}
	last := tokens[len(tokens)-1]
	switch target := parent.(type) {
	case map[string]any:
		if _, ok := target[last]; !ok {
			return fmt.Errorf("param override path %q not found", path)
		}
		delete(target, last)
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(target) {
			return fmt.Errorf("param override array index %q is invalid", last)
		}
		updated := make([]any, 0, len(target)-1)
		updated = append(updated, target[:index]...)
		updated = append(updated, target[index+1:]...)
		return pointerSet(document, pointerPath(tokens[:len(tokens)-1]), updated, false)
	default:
		return fmt.Errorf("param override path %q parent is not mutable", path)
	}
	return nil
}
