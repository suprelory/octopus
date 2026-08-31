package gemini

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func prepareGeminiRequest(request *model.InternalLLMRequest, effectiveModel string) (*model.InternalLLMRequest, model.AlternationReport) {
	if request == nil {
		return nil, model.AlternationReport{}
	}
	prepared := request.Clone()
	if modelName := strings.TrimSpace(effectiveModel); modelName != "" {
		prepared.Model = modelName
	}
	prepared.NormalizeMessages()
	messages, report := model.EnforceAlternationWithReport(prepared.Messages, model.AlternationProviderGemini)
	prepared.Messages = messages
	return prepared, report
}

func (o *MessagesOutbound) DescribeRequestChanges(request *model.InternalLLMRequest, effectiveModel string) []model.RequestTransformationChange {
	if request == nil {
		return nil
	}
	prepared, alternation := prepareGeminiRequest(request, effectiveModel)
	changes := alternation.RequestChanges("Gemini")
	for messageIndex, message := range prepared.Messages {
		for partIndex, part := range message.Content.MultipleContent {
			if strings.EqualFold(strings.TrimSpace(part.Type), "document") {
				field := fmt.Sprintf("messages[%d].content[%d]", messageIndex, partIndex)
				_, documentChanges, _ := planGeminiDocumentConversion(part.Document, prepared, field)
				changes = append(changes, documentChanges...)
			}
		}
	}
	return changes
}

func planGeminiDocumentConversion(doc *model.DocumentSource, req *model.InternalLLMRequest, field string) (*model.GeminiPart, []model.RequestTransformationChange, string) {
	change := func(action model.RequestTransformationAction, reason string) []model.RequestTransformationChange {
		if strings.TrimSpace(field) == "" {
			return nil
		}
		return []model.RequestTransformationChange{{Field: field, Action: action, Reason: reason}}
	}
	metadataChanges := func() []model.RequestTransformationChange {
		if doc == nil || (doc.Title == "" && doc.Context == "" && doc.Citations == nil) {
			return nil
		}
		return change(model.RequestTransformationDrop, "Gemini document parts do not preserve document title, context, or citation controls")
	}
	if doc == nil {
		return nil, change(model.RequestTransformationDrop, "Gemini drops a document content part with no document source"), ""
	}

	switch doc.Type {
	case "base64":
		if doc.Data == "" {
			return nil, change(model.RequestTransformationDrop, "Gemini drops an empty base64 document source"), ""
		}
		mime := doc.MediaType
		if mime == "" {
			mime = "application/pdf"
		}
		decoded := (len(doc.Data) * 3) / 4
		if decoded > geminiInlineDataMaxBytes {
			if uri := lookupGeminiFilesAPIURI(req, mime); uri != "" {
				warning := fmt.Sprintf("gemini: inline document ~%d bytes exceeds %d; forwarding via fileData(%q)", decoded, geminiInlineDataMaxBytes, uri)
				return &model.GeminiPart{FileData: &model.GeminiFileData{MimeType: mime, FileURI: uri}}, metadataChanges(), warning
			}
			reason := fmt.Sprintf("Gemini drops an inline document of approximately %d decoded bytes because it exceeds the %d-byte limit and no Files API URI is configured", decoded, geminiInlineDataMaxBytes)
			warning := fmt.Sprintf("gemini: dropping inline document (~%d bytes, mime=%q) - exceeds %d-byte inline limit and no gemini_files_api_uri provided", decoded, mime, geminiInlineDataMaxBytes)
			return nil, change(model.RequestTransformationDrop, reason), warning
		}
		return &model.GeminiPart{InlineData: &model.GeminiBlob{MimeType: mime, Data: doc.Data}}, metadataChanges(), ""

	case "url":
		if doc.URL == "" {
			return nil, change(model.RequestTransformationDrop, "Gemini drops an empty document URL source"), ""
		}
		reason := "Gemini converts an arbitrary document URL to a text hint and cannot preserve native document or citation semantics"
		return &model.GeminiPart{Text: buildDocumentTextHint(doc, "document at "+doc.URL)}, change(model.RequestTransformationTranslate, reason), ""

	case "text":
		text := doc.Text
		if text == "" {
			text = doc.Data
		}
		if text == "" {
			return nil, change(model.RequestTransformationDrop, "Gemini drops an empty text document source"), ""
		}
		reason := "Gemini converts a structured text document to plain text and cannot preserve citation controls"
		return &model.GeminiPart{Text: buildDocumentTextHint(doc, text)}, change(model.RequestTransformationTranslate, reason), ""

	case "content":
		if len(doc.Content) == 0 {
			return nil, change(model.RequestTransformationDrop, "Gemini drops an empty opaque document content source"), ""
		}
		return nil, change(model.RequestTransformationDrop, "Gemini cannot represent an opaque Anthropic document content array"), ""

	default:
		return nil, change(model.RequestTransformationDrop, fmt.Sprintf("Gemini cannot represent document source type %q", doc.Type)), ""
	}
}
