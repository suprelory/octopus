package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

func equalJSONValues(left, right string) bool {
	leftValue, err := decodeJSONValue(left)
	if err != nil {
		return false
	}
	rightValue, err := decodeJSONValue(right)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func decodeJSONValue(value string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("unexpected trailing JSON value: %w", err)
	}
	return decoded, nil
}

func (o *ResponseOutbound) marshalTrackedOutputItems() (json.RawMessage, bool) {
	if o == nil || len(o.outputItems) == 0 {
		return nil, false
	}
	maxIdx := -1
	for idx := range o.outputItems {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	items := make([]ResponsesItem, 0, len(o.outputItems))
	for idx := 0; idx <= maxIdx; idx++ {
		item, ok := o.outputItems[idx]
		if !ok {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, false
	}
	data, err := json.Marshal(sanitizeResponsesItems(items))
	if err != nil {
		return nil, false
	}
	return data, true
}

func sanitizeResponsesInput(input ResponsesInput) ResponsesInput {
	if len(input.Raw) > 0 {
		input.Raw = sanitizeResponsesRawItems(input.Raw)
	}
	if len(input.Items) > 0 {
		input.Items = sanitizeResponsesItems(input.Items)
	}
	return input
}

func sanitizeResponsesItems(items []ResponsesItem) []ResponsesItem {
	if len(items) == 0 {
		return items
	}

	sanitized := make([]ResponsesItem, len(items))
	for i, item := range items {
		sanitized[i] = item
		ensureResponsesReasoningSummary(&sanitized[i])
		ensureResponsesRefusalShape(&sanitized[i])
	}
	return sanitized
}

func ensureResponsesRefusalShape(item *ResponsesItem) {
	if item == nil || item.Content == nil {
		return
	}
	for i := range item.Content.Items {
		contentItem := &item.Content.Items[i]
		if contentItem.Type != "refusal" || contentItem.Refusal != nil || contentItem.Text == nil {
			continue
		}
		contentItem.Refusal = contentItem.Text
		contentItem.Text = nil
	}
}

func ensureResponsesReasoningSummary(item *ResponsesItem) {
	if item == nil || item.Type != "reasoning" {
		return
	}
	if len(item.Summary) == 0 {
		item.Summary = []ResponsesReasoningSummary{{
			Type: "summary_text",
			Text: "",
		}}
		return
	}
	for i := range item.Summary {
		if item.Summary[i].Type == "" {
			item.Summary[i].Type = "summary_text"
		}
		if item.Summary[i].Text == "" {
			item.Summary[i].Text = ""
		}
	}
}

func sanitizeResponsesRawItems(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}

	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw
	}

	changed := false

	// Build call_id -> item_id mapping from function_call items.
	// Generate an id for any function_call that has call_id but no id,
	// so the function_call_output backfill can always resolve item_reference.
	callIDToItemID := make(map[string]string)
	for _, item := range items {
		if decodeRawString(item["type"]) == "function_call" {
			callID := decodeRawString(item["call_id"])
			if callID == "" {
				continue
			}
			itemID := decodeRawString(item["id"])
			if itemID == "" {
				itemID = generateResponsesItemID()
				if b, err := json.Marshal(itemID); err == nil {
					item["id"] = b
					changed = true
				}
			}
			if itemID != "" {
				callIDToItemID[callID] = itemID
			}
		}
	}

	for _, item := range items {
		itemType := decodeRawString(item["type"])

		// Sanitize function_call_output: add missing item_reference
		if itemType == "function_call_output" {
			refRaw, hasRef := item["item_reference"]
			refMissing := !hasRef || len(bytes.TrimSpace(refRaw)) == 0 ||
				bytes.Equal(bytes.TrimSpace(refRaw), []byte("null")) ||
				bytes.Equal(bytes.TrimSpace(refRaw), []byte(`""`))
			if refMissing {
				callID := decodeRawString(item["call_id"])
				if callID != "" {
					if itemID, ok := callIDToItemID[callID]; ok {
						if b, err := json.Marshal(itemID); err == nil {
							item["item_reference"] = b
							changed = true
						}
					}
				}
			}
		}

		if itemType != "reasoning" {
			continue
		}

		summaryRaw, ok := item["summary"]
		if !ok || len(bytes.TrimSpace(summaryRaw)) == 0 || bytes.Equal(bytes.TrimSpace(summaryRaw), []byte("null")) {
			item["summary"] = defaultRawResponsesReasoningSummary()
			changed = true
			continue
		}

		sanitizedSummary, summaryChanged, ok := sanitizeResponsesRawSummary(summaryRaw)
		if ok && summaryChanged {
			item["summary"] = sanitizedSummary
			changed = true
		}
	}

	if !changed {
		return raw
	}

	data, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return data
}

func sanitizeResponsesRawSummary(raw json.RawMessage) (json.RawMessage, bool, bool) {
	var summaryItems []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &summaryItems); err != nil {
		return nil, false, false
	}
	if len(summaryItems) == 0 {
		return defaultRawResponsesReasoningSummary(), true, true
	}

	changed := false
	for _, summary := range summaryItems {
		typeRaw, hasType := summary["type"]
		if !hasType || len(bytes.TrimSpace(typeRaw)) == 0 || bytes.Equal(bytes.TrimSpace(typeRaw), []byte("null")) {
			summary["type"] = []byte(`"summary_text"`)
			changed = true
		}
		textRaw, hasText := summary["text"]
		if !hasText || len(bytes.TrimSpace(textRaw)) == 0 || bytes.Equal(bytes.TrimSpace(textRaw), []byte("null")) {
			summary["text"] = []byte(`""`)
			changed = true
		}
	}

	if !changed {
		return raw, false, true
	}

	data, err := json.Marshal(summaryItems)
	if err != nil {
		return nil, false, false
	}
	return data, true, true
}

func defaultRawResponsesReasoningSummary() json.RawMessage {
	return json.RawMessage(`[{"type":"summary_text","text":""}]`)
}

func decodeRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
