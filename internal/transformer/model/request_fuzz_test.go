package model

import (
	"bytes"
	"reflect"
	"testing"
)

func FuzzInternalRequestCloneIsolation(f *testing.F) {
	f.Add([]byte("hello"), "gpt-test")
	f.Add([]byte{0, 1, 2, 255}, "模型")
	f.Fuzz(func(t *testing.T, payload []byte, modelName string) {
		if len(payload) > 1<<20 {
			t.Skip()
		}
		text := string(payload)
		req := &InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: APIFormatOpenAIResponse,
			Operation: &RequestOperation{Responses: &ResponsesOperation{
				Messages: []Message{{
					Role: "user",
					Content: MessageContent{MultipleContent: []MessageContentPart{{
						Type: "text",
						Text: &text,
					}}},
				}},
			}},
			TransformerMetadata: map[string]string{"seed": text},
			RawRequest:          append([]byte(nil), payload...),
		}
		if err := req.Validate(); err != nil {
			return
		}
		clone := req.Clone()
		if !reflect.DeepEqual(req, clone) {
			t.Fatal("clone is not deeply equal to source")
		}

		clone.Model += "-mutated"
		clone.RawRequest = append(clone.RawRequest, 'x')
		clone.TransformerMetadata["seed"] = "mutated"
		clone.Operation.Responses.Messages[0].Content.MultipleContent[0].Text = stringPointer("mutated")
		if req.Model == clone.Model || bytes.Equal(req.RawRequest, clone.RawRequest) ||
			req.TransformerMetadata["seed"] == clone.TransformerMetadata["seed"] ||
			*req.Messages[0].Content.MultipleContent[0].Text == "mutated" {
			t.Fatal("mutating clone changed source request")
		}
	})
}

func stringPointer(value string) *string { return &value }
