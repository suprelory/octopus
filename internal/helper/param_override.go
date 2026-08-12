package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// ApplyParamOverride accepts both the legacy JSON object merge and the new
// operation-array form. Invalid override syntax is ignored for compatibility;
// valid operations return an error when they cannot be applied safely.
func ApplyParamOverride(request *http.Request, paramOverride *string) error {
	if request == nil || request.Body == nil || paramOverride == nil || strings.TrimSpace(*paramOverride) == "" {
		return nil
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	restoreBody := func(payload []byte) {
		request.Body = io.NopCloser(bytes.NewReader(payload))
		request.ContentLength = int64(len(payload))
		request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
	}
	restoreBody(body)

	var raw json.RawMessage = []byte(strings.TrimSpace(*paramOverride))
	var operations []ParamOverrideOperation
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &operations); err != nil {
			return nil
		}
		var document any
		if err := json.Unmarshal(body, &document); err != nil {
			return nil
		}
		for _, operation := range operations {
			if err := applyParamOperation(&document, operation); err != nil {
				return err
			}
		}
		modified, err := json.Marshal(document)
		if err != nil {
			return fmt.Errorf("failed to marshal request body with param operations: %w", err)
		}
		restoreBody(modified)
		return nil
	}

	var override map[string]any
	if err := json.Unmarshal(raw, &override); err != nil {
		return nil
	}
	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return nil
	}
	for key, value := range override {
		bodyMap[key] = value
	}
	modified, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal request body with param override: %w", err)
	}
	restoreBody(modified)
	return nil
}

func applyParamOperation(document *any, operation ParamOverrideOperation) error {
	op := strings.ToLower(strings.TrimSpace(operation.Op))
	if operation.Path == "" && op != "replace" && op != "add" {
		return fmt.Errorf("param override %s requires a path", operation.Op)
	}
	switch op {
	case "add", "replace", "set":
		return pointerSet(document, operation.Path, operation.Value, op == "add")
	case "remove", "delete":
		return pointerRemove(document, operation.Path)
	case "copy", "move":
		value, err := pointerGet(*document, operation.From)
		if err != nil {
			return err
		}
		if err := pointerSet(document, operation.Path, value, true); err != nil {
			return err
		}
		if op == "move" {
			return pointerRemove(document, operation.From)
		}
		return nil
	default:
		return fmt.Errorf("unsupported param override operation %q", operation.Op)
	}
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
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
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
		parent, err = pointerGet(*document, "/"+strings.Join(tokens[:len(tokens)-1], "/"))
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
				parentPath = "/" + strings.Join(tokens[:len(tokens)-1], "/")
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
		parent, err = pointerGet(*document, "/"+strings.Join(tokens[:len(tokens)-1], "/"))
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
		return pointerSet(document, "/"+strings.Join(tokens[:len(tokens)-1], "/"), append(target[:index], target[index+1:]...), false)
	default:
		return fmt.Errorf("param override path %q parent is not mutable", path)
	}
	return nil
}
