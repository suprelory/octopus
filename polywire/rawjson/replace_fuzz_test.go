package rawjson

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzReplaceTopLevelStringByteStability(f *testing.F) {
	f.Add([]byte(`{"model":"old","n":9007199254740993}`), "new")
	f.Add([]byte(" \n{\"nested\":{\"model\":\"keep\"},\"model\":\"\\u0061\"}\t"), "模型\n")
	f.Add([]byte(`{"model":"one","model":"two"}`), "reject")
	f.Fuzz(func(t *testing.T, raw []byte, replacement string) {
		if len(raw) > 1<<20 || len(replacement) > 1<<16 {
			t.Skip()
		}
		output, err := ReplaceTopLevelString(raw, "model", replacement)
		if err != nil {
			return
		}
		if !json.Valid(output) {
			t.Fatal("successful replacement produced invalid JSON")
		}
		start, end := topLevelStringBoundsForFuzz(t, raw, "model")
		outputStart, outputEnd := topLevelStringBoundsForFuzz(t, output, "model")
		if !bytes.Equal(raw[:start], output[:outputStart]) || !bytes.Equal(raw[end:], output[outputEnd:]) {
			t.Fatal("replacement changed bytes outside the target value token")
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(output, &decoded); err != nil {
			t.Fatal(err)
		}
		var got, normalizedReplacement string
		encodedReplacement, _ := json.Marshal(replacement)
		_ = json.Unmarshal(encodedReplacement, &normalizedReplacement)
		if err := json.Unmarshal(decoded["model"], &got); err != nil || got != normalizedReplacement {
			t.Fatalf("replacement value = %q, want %q, err = %v", got, normalizedReplacement, err)
		}
	})
}

func topLevelStringBoundsForFuzz(t *testing.T, raw []byte, field string) (int, int) {
	t.Helper()
	root := skipSpace(raw, 0)
	index := skipSpace(raw, root+1)
	for raw[index] != '}' {
		keyEnd := scanString(raw, index)
		var key string
		if err := json.Unmarshal(raw[index:keyEnd], &key); err != nil {
			t.Fatal(err)
		}
		index = skipSpace(raw, keyEnd)
		start := skipSpace(raw, index+1)
		end := scanValue(raw, start)
		if key == field {
			return start, end
		}
		index = skipSpace(raw, end)
		if raw[index] == ',' {
			index = skipSpace(raw, index+1)
		}
	}
	t.Fatalf("field %q not found after successful replacement", field)
	return 0, 0
}
