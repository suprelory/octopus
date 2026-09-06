package model

func cloneCitations(citations []Citation) []Citation {
	if citations == nil {
		return nil
	}
	cloned := make([]Citation, len(citations))
	for index, citation := range citations {
		cloned[index] = citation
		cloned[index].Raw = cloneRawMessage(citation.Raw)
	}
	return cloned
}

func cloneStreamContentPart(part MessageContentPart) MessageContentPart {
	if part.BlockIndex != nil {
		index := *part.BlockIndex
		part.BlockIndex = &index
	}
	if part.Text != nil {
		text := *part.Text
		part.Text = &text
	}
	part.Citations = cloneCitations(part.Citations)
	if part.ServerToolUse != nil {
		use := *part.ServerToolUse
		use.Input = cloneRawMessage(use.Input)
		use.Caller = cloneRawMessage(use.Caller)
		if use.InputDelta != nil {
			delta := *use.InputDelta
			use.InputDelta = &delta
			use.Input = []byte(delta)
		}
		part.ServerToolUse = &use
	}
	if part.ServerToolResult != nil {
		result := *part.ServerToolResult
		result.Content = cloneRawMessage(result.Content)
		if result.IsError != nil {
			isError := *result.IsError
			result.IsError = &isError
		}
		part.ServerToolResult = &result
	}
	return part
}

// mergeContentPartDelta keeps content in provider block order while merging
// text, citation and server-tool input fragments belonging to the same block.
func mergeContentPartDelta(parts []MessageContentPart, delta MessageContentPart) []MessageContentPart {
	for index := range parts {
		part := &parts[index]
		if part.Type != delta.Type || part.BlockIndex == nil || delta.BlockIndex == nil || *part.BlockIndex != *delta.BlockIndex {
			continue
		}
		if delta.Text != nil {
			text := *delta.Text
			if part.Text != nil {
				text = *part.Text + text
			}
			part.Text = &text
		}
		part.Citations = append(part.Citations, cloneCitations(delta.Citations)...)
		if delta.ServerToolUse != nil {
			if part.ServerToolUse == nil {
				part.ServerToolUse = cloneStreamContentPart(delta).ServerToolUse
				return parts
			}
			use, incoming := part.ServerToolUse, delta.ServerToolUse
			if incoming.ID != "" {
				use.ID = incoming.ID
			}
			if incoming.Name != "" {
				use.Name = incoming.Name
			}
			if incoming.BlockType != "" {
				use.BlockType = incoming.BlockType
			}
			if len(incoming.Caller) > 0 {
				use.Caller = cloneRawMessage(incoming.Caller)
			}
			if incoming.ServerName != "" {
				use.ServerName = incoming.ServerName
			}
			if incoming.InputDelta != nil {
				if use.InputDelta == nil {
					use.Input = nil // Replace the start block's {} placeholder.
				}
				use.Input = append(use.Input, (*incoming.InputDelta)...)
				input := string(use.Input)
				use.InputDelta = &input
			} else if len(incoming.Input) > 0 {
				use.Input = cloneRawMessage(incoming.Input)
			}
		}
		return parts
	}
	return append(parts, cloneStreamContentPart(delta))
}

func mergeMessageContentDelta(content *MessageContent, delta MessageContent) {
	if len(delta.MultipleContent) > 0 {
		if len(content.MultipleContent) == 0 && content.Content != nil && *content.Content != "" {
			text := *content.Content
			content.MultipleContent = append(content.MultipleContent, MessageContentPart{Type: "text", Text: &text})
		}
		for _, part := range delta.MultipleContent {
			content.MultipleContent = mergeContentPartDelta(content.MultipleContent, part)
		}
	} else if delta.Content != nil && len(content.MultipleContent) > 0 {
		content.MultipleContent = append(content.MultipleContent, cloneStreamContentPart(MessageContentPart{Type: "text", Text: delta.Content}))
	}
	if delta.Content != nil {
		text := *delta.Content
		if content.Content != nil {
			text = *content.Content + text
		}
		content.Content = &text
	}
}

func streamEventsFromContent(content MessageContent, citations []Citation, id, modelName string, choiceIndex int) []StreamEvent {
	var events []StreamEvent
	partCitations := 0
	hasTextParts := false
	for _, part := range content.MultipleContent {
		hasTextParts = hasTextParts || part.Type == "text"
		partCitations += len(part.Citations)
	}
	if !hasTextParts && content.Content != nil && *content.Content != "" {
		events = append(events, StreamEvent{Kind: StreamEventKindTextDelta, ID: id, Model: modelName, Index: choiceIndex, Delta: &StreamDelta{Text: *content.Content}})
	}
	for _, part := range content.MultipleContent {
		part = cloneStreamContentPart(part)
		event := StreamEvent{ID: id, Model: modelName, Index: choiceIndex, BlockIndex: part.BlockIndex}
		switch part.Type {
		case "text":
			if part.Text != nil && *part.Text != "" {
				event.Kind = StreamEventKindTextDelta
				event.Delta = &StreamDelta{Text: *part.Text}
				events = append(events, event)
			}
		case "server_tool_use":
			if part.ServerToolUse == nil {
				continue
			}
			use := part.ServerToolUse
			kind := use.BlockType
			if kind == "" {
				kind = "server_tool_use"
			}
			event.Kind = StreamEventKindContentBlockStart
			event.ContentBlock = &StreamContentBlock{Type: kind, ID: use.ID, Name: use.Name, Input: cloneRawMessage(use.Input), ServerToolUse: use}
			if use.InputDelta != nil {
				event.Kind = StreamEventKindContentBlockDelta
				event.Delta = &StreamDelta{Arguments: *use.InputDelta}
				use.Input = nil
				use.InputDelta = nil
				event.ContentBlock.Input = nil
			}
			events = append(events, event)
		case "server_tool_result":
			if part.ServerToolResult == nil {
				continue
			}
			result := part.ServerToolResult
			kind := result.BlockType
			if kind == "" {
				kind = "web_search_tool_result"
			}
			event.Kind = StreamEventKindContentBlockStart
			event.ContentBlock = &StreamContentBlock{Type: kind, ToolUseID: result.ToolUseID, IsError: result.IsError, ServerToolResult: result}
			events = append(events, event)
			event.Kind = StreamEventKindContentBlockStop
			events = append(events, event)
		}
		for _, citation := range part.Citations {
			events = append(events, StreamEvent{Kind: StreamEventKindCitationDelta, ID: id, Model: modelName, Index: choiceIndex, BlockIndex: part.BlockIndex, Delta: &StreamDelta{Citation: &citation}})
		}
	}
	if partCitations == 0 {
		for _, citation := range cloneCitations(citations) {
			events = append(events, StreamEvent{Kind: StreamEventKindCitationDelta, ID: id, Model: modelName, Index: choiceIndex, Delta: &StreamDelta{Citation: &citation}})
		}
	}
	return events
}
