package model

import "sort"

type streamBlockKey struct {
	choice int
	index  int
	tool   bool
}

func blockKey(event StreamEvent) streamBlockKey {
	key := streamBlockKey{choice: event.Index, index: -1}
	if event.BlockIndex != nil {
		key.index = *event.BlockIndex
	}
	if event.Kind == StreamEventKindToolCallStart || event.Kind == StreamEventKindToolCallDelta || event.Kind == StreamEventKindToolCallStop {
		key.tool = true
		if event.ToolCall != nil {
			key.index = event.ToolCall.Index
		}
	}
	return key
}

// normalizeBlock owns canonical block boundaries. Providers supply identities;
// encoders remain free to map these identities to their wire indices.
func (f *StreamFinalizer) normalizeBlock(event StreamEvent, events *[]StreamEvent) bool {
	key := blockKey(event)
	_, open := f.openBlocks[key]
	switch event.Kind {
	case StreamEventKindContentBlockStart, StreamEventKindToolCallStart:
		if open {
			return false
		}
		f.openBlocks[key] = event
	case StreamEventKindContentBlockStop, StreamEventKindToolCallStop:
		if !open {
			return false
		}
		delete(f.openBlocks, key)
	case StreamEventKindToolCallDelta:
		if !open && event.ToolCall != nil {
			start := event
			start.Kind, start.Delta = StreamEventKindToolCallStart, nil
			start = f.synthesized(start)
			tool := *event.ToolCall
			tool.Function.Arguments = ""
			start.ToolCall = &tool
			f.openBlocks[key] = start
			*events = append(*events, start)
			f.addToAggregate(start)
		}
	case StreamEventKindTextDelta, StreamEventKindThinkingDelta, StreamEventKindSignatureDelta, StreamEventKindCitationDelta:
		if !open && event.BlockIndex != nil {
			blockType := "text"
			if event.Kind == StreamEventKindThinkingDelta || event.Kind == StreamEventKindSignatureDelta {
				blockType = "thinking"
			}
			start := f.synthesized(StreamEvent{Kind: StreamEventKindContentBlockStart, ID: event.ID, Model: event.Model, Index: event.Index, BlockIndex: event.BlockIndex, ContentBlock: &StreamContentBlock{Type: blockType}})
			f.openBlocks[key] = start
			*events = append(*events, start)
			f.addToAggregate(start)
		}
	}
	return true
}

func (f *StreamFinalizer) closeChoiceBlocks(choice int, events *[]StreamEvent) {
	keys := make([]streamBlockKey, 0)
	for key := range f.openBlocks {
		if key.choice == choice {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].index != keys[j].index {
			return keys[i].index < keys[j].index
		}
		return !keys[i].tool && keys[j].tool
	})
	for _, key := range keys {
		stop := f.openBlocks[key]
		stop.Kind = StreamEventKindContentBlockStop
		if key.tool {
			stop.Kind = StreamEventKindToolCallStop
		}
		stop.ContentBlock, stop.Delta = nil, nil
		stop.Terminal, stop.TerminalEvent = false, ""
		stop = f.synthesized(stop)
		*events = append(*events, stop)
		f.addToAggregate(stop)
		delete(f.openBlocks, key)
	}
}

func duplicateTerminalBatch(events []StreamEvent) bool {
	terminal := false
	for _, event := range events {
		switch event.Kind {
		case StreamEventKindDone:
			terminal = true
		case StreamEventKindMessageStart, StreamEventKindMessageStop, StreamEventKindUsageDelta, StreamEventKindMessageMetadata, StreamEventKindResponseStop:
			terminal = terminal || event.Terminal
		default:
			return false
		}
	}
	return terminal
}
