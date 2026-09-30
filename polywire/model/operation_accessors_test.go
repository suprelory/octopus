package model

import (
	"encoding/json"
	"testing"
)

func TestOperationSettersKeepReplayAuthoritative(t *testing.T) {
	previous := "old_response"
	r := &InternalLLMRequest{Model: "m", RawAPIFormat: APIFormatOpenAIResponse, Operation: &RequestOperation{Responses: &ResponsesOperation{RawInputItems: json.RawMessage(`[{"type":"item_reference","id":"old"}]`), PreviousResponseID: &previous}}}
	if err := r.NormalizeOperation(); err != nil {
		t.Fatal(err)
	}
	options := r.GetOpenAIResponsesOptions()
	options.PreviousResponseID = nil
	options.RawInputItems = nil
	r.SetOpenAIResponsesOptions(options)
	r.SetOpenAIRawInputItems(json.RawMessage(`[{"role":"user","content":"replayed"}]`))
	if err := r.NormalizeOperation(); err != nil {
		t.Fatal(err)
	}
	if r.OpenAIPreviousResponseID() != "" || string(r.OpenAIRawInputItems()) != `[{"role":"user","content":"replayed"}]` || r.ResponsesPayload().PreviousResponseID != nil {
		t.Fatalf("stale replay fields: %+v", r.ResponsesPayload())
	}
}

func TestOperationAccessorsDoNotRecoverStaleLegacyData(t *testing.T) {
	text := "canonical"
	r := &InternalLLMRequest{Operation: &RequestOperation{Responses: &ResponsesOperation{Messages: []Message{{Role: "user", Content: MessageContent{Content: &text}}}}}, ProviderExtensions: &ProviderExtensions{OpenAI: &OpenAIExtension{RawResponseItems: json.RawMessage(`[{"id":"stale"}]`), Responses: &OpenAIResponsesOptions{RawInputItems: json.RawMessage(`[{"id":"stale"}]`)}}}}
	if len(r.GetOpenAIExtensions().RawResponseItems) != 0 || len(r.GetOpenAIResponsesOptions().RawInputItems) != 0 {
		t.Fatal("stale sidecar overrode operation")
	}
	if r.ChatPayload() != nil || r.EmbeddingsPayload() != nil || r.ImagesPayload() != nil || r.RerankPayload() != nil {
		t.Fatal("wrong operation accessor returned payload")
	}
	images := &InternalLLMRequest{Operation: &RequestOperation{Images: &ImagesOperation{Prompt: "octopus"}}}
	rerank := &InternalLLMRequest{Operation: &RequestOperation{Rerank: &RerankOperation{Query: "q", Documents: []RerankDocument{{Text: "d"}}}}}
	if images.ImagesPayload().Prompt != "octopus" || rerank.RerankPayload().Query != "q" {
		t.Fatal("operation accessors lost payload")
	}
}
