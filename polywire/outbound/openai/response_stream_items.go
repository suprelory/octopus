package openai

import (
	"encoding/json"
	"strings"

	"github.com/samber/lo"
)

func (o *ResponseOutbound) ensureOutputItem(outputIndex int, itemType string) ResponsesItem {
	if o.outputItems == nil {
		o.outputItems = make(map[int]ResponsesItem)
	}
	item := o.outputItems[outputIndex]
	if item.Type == "" && itemType != "" {
		item.Type = itemType
	}
	if item.Type == "message" && item.Content == nil {
		item.Content = &ResponsesInput{}
	}
	if item.Type == "reasoning" {
		ensureResponsesReasoningSummary(&item)
	}
	o.outputItems[outputIndex] = item
	return item
}

func cloneResponsesInput(input *ResponsesInput) *ResponsesInput {
	if input == nil {
		return nil
	}
	cloned := *input
	cloned.Items = append([]ResponsesItem(nil), input.Items...)
	if len(input.Raw) > 0 {
		cloned.Raw = append(json.RawMessage(nil), input.Raw...)
	}
	return &cloned
}

func cloneResponsesItem(item ResponsesItem) ResponsesItem {
	cloned := item
	cloned.Content = cloneResponsesInput(item.Content)
	cloned.Output = cloneResponsesInput(item.Output)
	cloned.Summary = append([]ResponsesReasoningSummary(nil), item.Summary...)
	return cloned
}

func mergeResponsesInputPreservingText(dst, src *ResponsesInput) *ResponsesInput {
	if dst == nil && src == nil {
		return nil
	}
	if dst == nil {
		return cloneResponsesInput(src)
	}
	if src == nil {
		return dst
	}
	if dst.Text == nil && src.Text != nil {
		text := *src.Text
		dst.Text = &text
	}
	if len(dst.Items) == 0 && len(src.Items) > 0 {
		dst.Items = append([]ResponsesItem(nil), src.Items...)
	} else {
		for i := range src.Items {
			if i >= len(dst.Items) {
				dst.Items = append(dst.Items, src.Items[i:]...)
				break
			}
			if dst.Items[i].Type == "" {
				dst.Items[i].Type = src.Items[i].Type
			}
			if dst.Items[i].Text == nil && src.Items[i].Text != nil {
				text := *src.Items[i].Text
				dst.Items[i].Text = &text
			}
		}
	}
	if len(dst.Raw) == 0 && len(src.Raw) > 0 {
		dst.Raw = append(json.RawMessage(nil), src.Raw...)
	}
	return dst
}

func (o *ResponseOutbound) mergeOutputItemAdded(event ResponsesStreamEvent) {
	if o == nil || event.Item == nil {
		return
	}
	if o.outputItems == nil {
		o.outputItems = make(map[int]ResponsesItem)
	}
	cloned := cloneResponsesItem(*event.Item)
	if existing, ok := o.outputItems[event.OutputIndex]; ok {
		if cloned.Type == "" {
			cloned.Type = existing.Type
		}
		cloned.Content = mergeResponsesInputPreservingText(cloned.Content, existing.Content)
		cloned.Output = mergeResponsesInputPreservingText(cloned.Output, existing.Output)
		if len(cloned.Summary) == 0 && len(existing.Summary) > 0 {
			cloned.Summary = append([]ResponsesReasoningSummary(nil), existing.Summary...)
		}
		cloned.CallID = firstNonEmpty(cloned.CallID, existing.CallID)
		cloned.Name = firstNonEmpty(cloned.Name, existing.Name)
		cloned.Namespace = firstNonEmpty(cloned.Namespace, existing.Namespace)
		if cloned.Arguments == "" {
			cloned.Arguments = existing.Arguments
		} else if existing.Arguments != "" && !strings.Contains(cloned.Arguments, existing.Arguments) {
			cloned.Arguments = existing.Arguments + cloned.Arguments
		}
	}
	o.outputItems[event.OutputIndex] = cloned
}

func (o *ResponseOutbound) mergeOutputTextDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	item := o.ensureOutputItem(event.OutputIndex, "message")
	if item.Type != "message" {
		return
	}
	if item.Content == nil {
		item.Content = &ResponsesInput{}
	}
	contentIndex := 0
	if event.ContentIndex != nil && *event.ContentIndex >= 0 {
		contentIndex = *event.ContentIndex
	}
	for len(item.Content.Items) <= contentIndex {
		item.Content.Items = append(item.Content.Items, ResponsesItem{})
	}
	if item.Content.Items[contentIndex].Type == "" {
		item.Content.Items[contentIndex].Type = "output_text"
	}
	if item.Content.Items[contentIndex].Text == nil {
		item.Content.Items[contentIndex].Text = lo.ToPtr("")
	}
	*item.Content.Items[contentIndex].Text += event.Delta
	o.outputItems[event.OutputIndex] = item
}

func (o *ResponseOutbound) mergeFunctionCallDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	item := o.ensureOutputItem(event.OutputIndex, "function_call")
	if item.Type != "function_call" {
		return
	}
	item.CallID = firstNonEmpty(item.CallID, event.CallID)
	item.Name = firstNonEmpty(item.Name, event.Name)
	item.Namespace = firstNonEmpty(item.Namespace, event.Namespace)
	item.Arguments += event.Delta
	o.outputItems[event.OutputIndex] = item
}

func (o *ResponseOutbound) mergeReasoningDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	item := o.ensureOutputItem(event.OutputIndex, "reasoning")
	if item.Type != "reasoning" {
		return
	}
	summaryIndex := 0
	if event.SummaryIndex != nil && *event.SummaryIndex >= 0 {
		summaryIndex = *event.SummaryIndex
	}
	for len(item.Summary) <= summaryIndex {
		item.Summary = append(item.Summary, ResponsesReasoningSummary{})
	}
	if item.Summary[summaryIndex].Type == "" {
		item.Summary[summaryIndex].Type = "summary_text"
	}
	item.Summary[summaryIndex].Text += event.Delta
	o.outputItems[event.OutputIndex] = item
}

const maxResponsesReasoningContentParts = 1024

func validResponsesReasoningContentIndex(index int) bool {
	return index >= 0 && index < maxResponsesReasoningContentParts
}

func (o *ResponseOutbound) mergeReasoningTextDelta(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	contentIndex := 0
	if event.ContentIndex != nil {
		contentIndex = *event.ContentIndex
	}
	if !validResponsesReasoningContentIndex(contentIndex) {
		return
	}

	item := o.ensureOutputItem(event.OutputIndex, "reasoning")
	if item.Type != "reasoning" {
		return
	}
	if item.Content == nil {
		item.Content = &ResponsesInput{}
	}
	for len(item.Content.Items) <= contentIndex {
		item.Content.Items = append(item.Content.Items, ResponsesItem{})
	}
	part := &item.Content.Items[contentIndex]
	if part.Type == "" {
		part.Type = "reasoning_text"
	}
	if part.Text == nil {
		part.Text = lo.ToPtr("")
	}
	*part.Text += event.Delta
	o.outputItems[event.OutputIndex] = item
}

func (o *ResponseOutbound) mergeReasoningTextDone(event ResponsesStreamEvent) {
	if o == nil {
		return
	}
	contentIndex := 0
	if event.ContentIndex != nil {
		contentIndex = *event.ContentIndex
	}
	if !validResponsesReasoningContentIndex(contentIndex) {
		return
	}

	item := o.ensureOutputItem(event.OutputIndex, "reasoning")
	if item.Type != "reasoning" {
		return
	}
	if item.Content == nil {
		item.Content = &ResponsesInput{}
	}
	for len(item.Content.Items) <= contentIndex {
		item.Content.Items = append(item.Content.Items, ResponsesItem{})
	}
	part := &item.Content.Items[contentIndex]
	if part.Type == "" {
		part.Type = "reasoning_text"
	}
	if event.Text != "" {
		part.Text = lo.ToPtr(event.Text)
	} else if part.Text == nil {
		part.Text = lo.ToPtr("")
	}
	o.outputItems[event.OutputIndex] = item
}
