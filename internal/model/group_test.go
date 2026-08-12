package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGroupEmptyResponseDetectionIsNotExposed(t *testing.T) {
	values := []any{
		Group{EmptyResponseDetection: true},
		GroupPreset{EmptyResponseDetection: true},
	}

	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
		if strings.Contains(string(encoded), "empty_response_detection") {
			t.Fatalf("%T still exposes legacy empty response detection field: %s", value, encoded)
		}
	}
}
