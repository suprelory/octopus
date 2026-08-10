package model

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestInternalLLMRequestCloneDeepCopiesRuntimeAndNestedFields(t *testing.T) {
	content := "hello"
	detail := "high"
	cacheRef := "cachedContents/1"
	request := &InternalLLMRequest{
		RequestType: RequestTypeChat,
		Model:       "source-model",
		Messages: []Message{{
			Role: "user",
			Content: MessageContent{MultipleContent: []MessageContentPart{{
				Type:     "image_url",
				Text:     &content,
				ImageURL: &ImageURL{URL: "https://example.test/image", Detail: &detail},
				Document: &DocumentSource{Type: "content", Content: json.RawMessage(`[{"type":"text"}]`)},
			}}},
			ProviderExtensions: &ProviderExtensions{Anthropic: &AnthropicExtension{Beta: []string{"feature"}}},
		}},
		RawRequest:          []byte("raw-request"),
		TransformerMetadata: map[string]string{"mode": "original"},
		Query:               url.Values{"include": {"usage", "trace"}},
		ProviderExtensions: &ProviderExtensions{
			Common: &CommonExtension{Raw: json.RawMessage(`{"nested":[1,2]}`)},
			Gemini: &GeminiExtension{CachedContentRef: &cacheRef},
		},
		ResponseFormat: &ResponseFormat{
			Schema: &Schema{
				Type:       "object",
				Properties: map[string]*Schema{"value": {Type: "string"}},
				Default:    map[string]any{"items": []any{"one", map[string]any{"two": 2}}},
			},
			RawSchema: json.RawMessage(`{"type":"object"}`),
		},
	}

	cloned := request.Clone()
	if cloned == request || !reflect.DeepEqual(cloned, request) {
		t.Fatalf("Clone() = %#v, want independent deeply equal request", cloned)
	}

	cloned.Messages[0].Content.MultipleContent[0].ImageURL.URL = "changed"
	cloned.Messages[0].Content.MultipleContent[0].Document.Content[0] = '['
	cloned.Messages[0].ProviderExtensions.Anthropic.Beta[0] = "changed"
	cloned.RawRequest[0] = 'R'
	cloned.TransformerMetadata["mode"] = "changed"
	cloned.Query["include"][0] = "changed"
	cloned.ProviderExtensions.Common.Raw[0] = '['
	*cloned.ProviderExtensions.Gemini.CachedContentRef = "changed"
	cloned.ResponseFormat.Schema.Properties["value"].Type = "number"
	cloned.ResponseFormat.Schema.Default.(map[string]any)["items"].([]any)[1].(map[string]any)["two"] = 3
	cloned.ResponseFormat.RawSchema[0] = '['

	if request.Messages[0].Content.MultipleContent[0].ImageURL.URL != "https://example.test/image" ||
		string(request.Messages[0].Content.MultipleContent[0].Document.Content) != `[{"type":"text"}]` ||
		request.Messages[0].ProviderExtensions.Anthropic.Beta[0] != "feature" ||
		string(request.RawRequest) != "raw-request" ||
		request.TransformerMetadata["mode"] != "original" ||
		request.Query["include"][0] != "usage" ||
		string(request.ProviderExtensions.Common.Raw) != `{"nested":[1,2]}` ||
		*request.ProviderExtensions.Gemini.CachedContentRef != "cachedContents/1" ||
		request.ResponseFormat.Schema.Properties["value"].Type != "string" ||
		request.ResponseFormat.Schema.Default.(map[string]any)["items"].([]any)[1].(map[string]any)["two"] != 2 ||
		string(request.ResponseFormat.RawSchema) != `{"type":"object"}` {
		t.Fatal("mutating clone changed the source request")
	}
}

func TestInternalLLMRequestClonePreservesCycles(t *testing.T) {
	schema := &Schema{Type: "object"}
	schema.Items = schema
	request := &InternalLLMRequest{Model: "model", ResponseFormat: &ResponseFormat{Schema: schema}}

	cloned := request.Clone()
	if cloned.ResponseFormat.Schema == schema {
		t.Fatal("schema pointer was shared with source")
	}
	if cloned.ResponseFormat.Schema.Items != cloned.ResponseFormat.Schema {
		t.Fatal("schema cycle was not preserved")
	}
}

func TestCloneMessagesPreservesNilAndCopiesElements(t *testing.T) {
	if CloneMessages(nil) != nil {
		t.Fatal("CloneMessages(nil) must return nil")
	}
	text := "hello"
	messages := []Message{{Content: MessageContent{Content: &text}}}
	cloned := CloneMessages(messages)
	*cloned[0].Content.Content = "changed"
	if *messages[0].Content.Content != "hello" {
		t.Fatal("mutating cloned message changed source")
	}
}
