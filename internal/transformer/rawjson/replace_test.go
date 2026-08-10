package rawjson

import (
	"bytes"
	"testing"
)

func TestReplaceTopLevelStringPreservesBytesOutsideValue(t *testing.T) {
	raw := []byte(" \n{\n  \"input\": 9007199254740993,\n  \"metadata\": {\"model\": \"nested\"},\n  \"model\": \"client\\nmodel\",\n  \"tail\": \"\\u0061\"\n} \t\n")
	want := []byte(" \n{\n  \"input\": 9007199254740993,\n  \"metadata\": {\"model\": \"nested\"},\n  \"model\": \"upstream\\\"model\",\n  \"tail\": \"\\u0061\"\n} \t\n")

	got, err := ReplaceTopLevelString(raw, "model", "upstream\"model")
	if err != nil {
		t.Fatalf("ReplaceTopLevelString() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("replacement changed unrelated bytes\ngot:  %s\nwant: %s", got, want)
	}
}

func TestReplaceTopLevelStringRejectsAmbiguousOrInvalidTargets(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing", raw: `{"input":"hello"}`},
		{name: "duplicate", raw: `{"model":"one","model":"two"}`},
		{name: "non-string", raw: `{"model":123}`},
		{name: "nested only", raw: `{"input":{"model":"nested"}}`},
		{name: "non-object", raw: `[]`},
		{name: "invalid", raw: `{"model":"unterminated}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReplaceTopLevelString([]byte(tt.raw), "model", "upstream"); err == nil {
				t.Fatal("ReplaceTopLevelString() error = nil")
			}
		})
	}
}
