// Package rawjson contains byte-preserving operations for validated JSON
// payloads used by same-protocol passthrough paths.
package rawjson

import (
	"encoding/json"
	"fmt"
)

// ReplaceTopLevelString replaces the unique top-level object field named
// field. The field must exist exactly once and its value must be a JSON
// string. Apart from that string token, the returned bytes are identical to
// raw.
func ReplaceTopLevelString(raw []byte, field, value string) ([]byte, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid JSON")
	}

	index := skipSpace(raw, 0)
	if index >= len(raw) || raw[index] != '{' {
		return nil, fmt.Errorf("JSON root must be an object")
	}
	index = skipSpace(raw, index+1)

	count := 0
	valueStart, valueEnd := 0, 0
	for index < len(raw) && raw[index] != '}' {
		if raw[index] != '"' {
			return nil, fmt.Errorf("invalid JSON object key")
		}
		keyStart := index
		keyEnd := scanString(raw, index)
		var key string
		if err := json.Unmarshal(raw[keyStart:keyEnd], &key); err != nil {
			return nil, fmt.Errorf("invalid JSON object key: %w", err)
		}

		index = skipSpace(raw, keyEnd)
		if index >= len(raw) || raw[index] != ':' {
			return nil, fmt.Errorf("invalid JSON object: missing colon")
		}
		start := skipSpace(raw, index+1)
		end := scanValue(raw, start)
		if key == field {
			count++
			if raw[start] != '"' {
				return nil, fmt.Errorf("top-level field %q must be a string", field)
			}
			valueStart, valueEnd = start, end
		}

		index = skipSpace(raw, end)
		if index >= len(raw) {
			return nil, fmt.Errorf("invalid JSON object")
		}
		switch raw[index] {
		case ',':
			index = skipSpace(raw, index+1)
		case '}':
			// The loop terminates on the closing delimiter.
		default:
			return nil, fmt.Errorf("invalid JSON object")
		}
	}
	if index >= len(raw) || raw[index] != '}' {
		return nil, fmt.Errorf("invalid JSON object")
	}
	if skipSpace(raw, index+1) != len(raw) {
		return nil, fmt.Errorf("invalid trailing JSON data")
	}
	if count == 0 {
		return nil, fmt.Errorf("top-level field %q is missing", field)
	}
	if count > 1 {
		return nil, fmt.Errorf("top-level field %q is duplicated", field)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to encode replacement value: %w", err)
	}
	result := make([]byte, 0, len(raw)-(valueEnd-valueStart)+len(encoded))
	result = append(result, raw[:valueStart]...)
	result = append(result, encoded...)
	result = append(result, raw[valueEnd:]...)
	return result, nil
}

func skipSpace(raw []byte, index int) int {
	for index < len(raw) {
		switch raw[index] {
		case ' ', '\t', '\r', '\n':
			index++
		default:
			return index
		}
	}
	return index
}

func scanString(raw []byte, index int) int {
	index++
	for index < len(raw) {
		switch raw[index] {
		case '"':
			return index + 1
		case '\\':
			index += 2
		default:
			index++
		}
	}
	return len(raw)
}

func scanValue(raw []byte, index int) int {
	switch raw[index] {
	case '"':
		return scanString(raw, index)
	case '{', '[':
		depth := 0
		for index < len(raw) {
			switch raw[index] {
			case '"':
				index = scanString(raw, index)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return index + 1
				}
			}
			index++
		}
		return len(raw)
	default:
		for index < len(raw) {
			switch raw[index] {
			case ' ', '\t', '\r', '\n', ',', '}', ']':
				return index
			default:
				index++
			}
		}
		return index
	}
}
