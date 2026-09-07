package op

import (
	"encoding/json"
	"fmt"
	"strings"
)

func asObject(value any) rawImportObject {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func asObjectFromJSONString(value string) rawImportObject {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var result rawImportObject
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}

func asObjectSlice(value any) []rawImportObject {
	typed, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]rawImportObject, 0, len(typed))
	for _, item := range typed {
		if row := asObject(item); row != nil {
			result = append(result, row)
		}
	}
	return result
}

func asStringPointer(value any) *string {
	trimmed := asString(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case int:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	case int64:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	default:
		return ""
	}
}

func asBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case float64:
		return typed != 0
	case int:
		return typed != 0
	}
	return fallback
}

func asIntPointer(value any) *int {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return &typed
		}
	case int64:
		if typed > 0 {
			result := int(typed)
			return &result
		}
	case float64:
		if typed > 0 {
			result := int(typed)
			return &result
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			result := int(parsed)
			return &result
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Int64(); err == nil && parsed > 0 {
			result := int(parsed)
			return &result
		}
	}
	return nil
}

func asInt(value any) int {
	if parsed := asInt64(value); parsed > 0 {
		return int(parsed)
	}
	return 0
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return int64(typed)
		}
	case int64:
		if typed > 0 {
			return typed
		}
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return parsed
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Int64(); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func asFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return typed
		}
	case float32:
		if typed > 0 {
			return float64(typed)
		}
	case int:
		if typed > 0 {
			return float64(typed)
		}
	case int64:
		if typed > 0 {
			return float64(typed)
		}
	case json.Number:
		if parsed, err := typed.Float64(); err == nil && parsed > 0 {
			return parsed
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Float64(); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
