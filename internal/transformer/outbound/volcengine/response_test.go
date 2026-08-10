package volcengine

import (
	"context"
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
