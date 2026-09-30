package model

import "strings"

type streamContentKey struct {
	typ   string
	index int
}

type streamContentAggregate struct {
	text    strings.Builder
	hasText bool
	parts   []*streamPartAggregate
	indices map[streamContentKey]int
}

type streamPartAggregate struct {
	part       MessageContentPart
	text       strings.Builder
	hasText    bool
	inputDelta bool
}

func (s *streamContentAggregate) add(delta MessageContent) {
	if len(delta.MultipleContent) > 0 {
		if len(s.parts) == 0 && s.text.Len() > 0 {
			text := s.text.String()
			s.appendPart(MessageContentPart{Type: "text", Text: &text})
		}
		for _, part := range delta.MultipleContent {
			if part.BlockIndex != nil {
				if index, exists := s.indices[streamContentKey{part.Type, *part.BlockIndex}]; exists {
					s.parts[index].add(part)
					continue
				}
			}
			s.appendPart(part)
		}
	} else if delta.Content != nil && len(s.parts) > 0 {
		s.appendPart(MessageContentPart{Type: "text", Text: delta.Content})
	}
	if delta.Content != nil {
		s.hasText = true
		s.text.WriteString(*delta.Content)
	}
}

func (s *streamContentAggregate) appendPart(delta MessageContentPart) {
	part := deepClone(delta).(MessageContentPart)
	state := &streamPartAggregate{part: part}
	if part.Text != nil {
		state.hasText = true
		state.text.WriteString(*part.Text)
		state.part.Text = nil
	}
	if use := part.ServerToolUse; use != nil && use.InputDelta != nil {
		use.Input = []byte(*use.InputDelta)
		use.InputDelta = nil
		state.inputDelta = true
	}
	if part.BlockIndex != nil {
		if s.indices == nil {
			s.indices = make(map[streamContentKey]int)
		}
		key := streamContentKey{part.Type, *part.BlockIndex}
		if _, exists := s.indices[key]; !exists {
			s.indices[key] = len(s.parts)
		}
	}
	s.parts = append(s.parts, state)
}

func (s *streamPartAggregate) add(delta MessageContentPart) {
	if delta.Text != nil {
		s.hasText = true
		s.text.WriteString(*delta.Text)
	}
	s.part.Citations = append(s.part.Citations, cloneCitations(delta.Citations)...)
	if delta.ServerToolUse == nil {
		return
	}
	if s.part.ServerToolUse == nil {
		s.part.ServerToolUse = &ServerToolUseBlock{}
	}
	use, incoming := s.part.ServerToolUse, delta.ServerToolUse
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
		if !s.inputDelta {
			use.Input = nil // Replace the start block's {} placeholder once.
			s.inputDelta = true
		}
		use.Input = append(use.Input, (*incoming.InputDelta)...)
	} else if len(incoming.Input) > 0 {
		use.Input = cloneRawMessage(incoming.Input)
	}
}

func (s *streamContentAggregate) build() MessageContent {
	content := MessageContent{}
	if s.hasText {
		text := s.text.String()
		content.Content = &text
	}
	if len(s.parts) > 0 {
		content.MultipleContent = make([]MessageContentPart, len(s.parts))
		for index, state := range s.parts {
			part := state.part
			if state.hasText {
				text := state.text.String()
				part.Text = &text
			}
			content.MultipleContent[index] = part
		}
	}
	return content
}
