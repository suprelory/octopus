package model

import "testing"

func TestCaptureFieldPresenceDistinguishesAbsentNullAndEmpty(t *testing.T) {
	request := &InternalLLMRequest{}
	if err := request.CaptureFieldPresence([]byte(`{
		"model":"m",
		"temperature":null,
		"metadata":{},
		"stop":[],
		"user":""
	}`)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		field string
		want  FieldPresence
	}{
		{field: "missing", want: FieldAbsent},
		{field: "temperature", want: FieldExplicitNull},
		{field: "metadata", want: FieldPresent},
		{field: "stop", want: FieldPresent},
		{field: "user", want: FieldPresent},
	}
	for _, test := range tests {
		if got := request.FieldPresenceOf(test.field); got != test.want {
			t.Errorf("FieldPresenceOf(%q) = %d, want %d", test.field, got, test.want)
		}
	}
}

func TestSetFieldPresenceCanClearState(t *testing.T) {
	request := &InternalLLMRequest{}
	request.SetFieldPresence("temperature", FieldExplicitNull)
	request.SetFieldPresence("temperature", FieldAbsent)
	if got := request.FieldPresenceOf("temperature"); got != FieldAbsent {
		t.Fatalf("FieldPresenceOf(temperature) = %d, want absent", got)
	}
}
