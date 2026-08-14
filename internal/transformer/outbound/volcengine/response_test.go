package volcengine

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestResponseOutboundDoesNotMutateRequest(t *testing.T) {
	for name, baseURL := range map[string]string{"success": "https://ark.cn-beijing.volces.com/api/v3", "failure": "http://[::1"} {
		t.Run(name, func(t *testing.T) {
			empty := ""
			text := "hello"
			request := &model.InternalLLMRequest{
				Model: "doubao-test",
				Messages: []model.Message{{
					Role: "user",
					Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
						{Type: "text", Text: &empty},
						{Type: "text", Text: &text},
					}},
				}},
				TransformerMetadata: map[string]string{"source": "client"},
			}
			before := request.Clone()
			_, err := (&ResponseOutbound{}).TransformRequest(context.Background(), request, baseURL, "key")
			if name == "failure" && err == nil {
				t.Fatal("TransformRequest() error = nil, want invalid URL error")
			}
			if name == "success" && err != nil {
				t.Fatalf("TransformRequest() error = %v", err)
			}
			if !reflect.DeepEqual(request, before) {
				t.Fatalf("TransformRequest() mutated caller request\nbefore: %#v\nafter:  %#v", before, request)
			}
		})
	}
}

func TestResponseOutboundHandlesNormalizedEmptyMessages(t *testing.T) {
	empty := ""
	request := &model.InternalLLMRequest{
		Model: "doubao-test",
		Messages: []model.Message{{
			Content: model.MessageContent{Content: &empty},
		}},
	}
	httpRequest, err := (&ResponseOutbound{}).TransformRequest(context.Background(), request, "https://ark.cn-beijing.volces.com/api/v3", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer httpRequest.Body.Close()
	body, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload["input"]) != "[]" {
		t.Fatalf("input = %s, want []", payload["input"])
	}
}

func TestResponseOutboundPreservesRawInputItems(t *testing.T) {
	request := &model.InternalLLMRequest{
		Model:        "doubao-test",
		RequestType:  model.RequestTypeResponses,
		RawAPIFormat: model.APIFormatOpenAIResponse,
	}
	request.SetOpenAIRawInputItems(json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}],"native_meta":{"keep":true}},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"draft"}],"native_meta":{"keep":true}}
	]`))

	httpRequest, err := (&ResponseOutbound{}).TransformRequest(context.Background(), request, "https://ark.cn-beijing.volces.com/api/v3", "key")
	if err != nil {
		t.Fatal(err)
	}
	defer httpRequest.Body.Close()
	body, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 2 {
		t.Fatalf("input = %#v, want two raw items", payload.Input)
	}
	if _, ok := payload.Input[0]["native_meta"]; !ok {
		t.Fatalf("first raw item lost unknown fields: %#v", payload.Input[0])
	}
	if _, ok := payload.Input[1]["native_meta"]; !ok {
		t.Fatalf("last raw item lost unknown fields: %#v", payload.Input[1])
	}
	if partial, ok := payload.Input[1]["partial"].(bool); !ok || !partial {
		t.Fatalf("last assistant item partial = %#v, want true", payload.Input[1]["partial"])
	}
}
