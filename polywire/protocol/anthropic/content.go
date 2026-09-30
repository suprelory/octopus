package anthropic

import (
	"encoding/json"
	"strings"
)

func IsServerToolUse(kind string) bool {
	return strings.HasSuffix(kind, "_tool_use") && kind != "tool_use"
}

func IsServerToolResult(kind string) bool {
	return strings.HasSuffix(kind, "_tool_result") && kind != "tool_result"
}

func (b *MessageContentBlock) UnmarshalJSON(data []byte) error {
	type alias MessageContentBlock
	var decoded alias
	aux := struct {
		*alias
		Content   json.RawMessage `json:"content"`
		Citations json.RawMessage `json:"citations"`
	}{alias: &decoded}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.Content) > 0 {
		if IsServerToolResult(decoded.Type) {
			// Result payloads include objects and nested blocks outside this DTO.
			// Do not decode them through the text/image content union.
			decoded.RawContent = aux.Content
		} else if err := json.Unmarshal(aux.Content, &decoded.Content); err != nil {
			return err
		}
	}
	if len(aux.Citations) > 0 {
		if decoded.Type == "document" {
			if err := json.Unmarshal(aux.Citations, &decoded.CitationConfig); err != nil {
				return err
			}
		} else if err := json.Unmarshal(aux.Citations, &decoded.Citations); err != nil {
			return err
		}
	}
	*b = MessageContentBlock(decoded)
	return nil
}

func (b MessageContentBlock) MarshalJSON() ([]byte, error) {
	type alias MessageContentBlock
	var content, citations any
	if len(b.RawContent) > 0 {
		content = b.RawContent
	} else if b.Content != nil {
		content = b.Content
	}
	if b.Type == "document" {
		if b.CitationConfig != nil {
			citations = b.CitationConfig
		}
	} else if b.Citations != nil {
		citations = b.Citations
	}
	return json.Marshal(struct {
		alias
		Content   any `json:"content,omitempty"`
		Citations any `json:"citations,omitempty"`
	}{alias: alias(b), Content: content, Citations: citations})
}

func (c *Citation) UnmarshalJSON(data []byte) error {
	type alias Citation
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Citation(decoded)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (c Citation) MarshalJSON() ([]byte, error) {
	if len(c.Raw) > 0 {
		return c.Raw, nil
	}
	type alias Citation
	data, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	putIndex := func(name string, value int) {
		fields[name], _ = json.Marshal(value)
	}
	// Native citation locations are zero-based. Synthesized citations must
	// include their required zero-valued fields as well.
	switch c.Type {
	case "char_location":
		putIndex("document_index", c.DocumentIndex)
		putIndex("start_char_index", c.StartCharIndex)
		putIndex("end_char_index", c.EndCharIndex)
	case "page_location":
		putIndex("document_index", c.DocumentIndex)
		putIndex("start_page_number", c.StartPageNumber)
		putIndex("end_page_number", c.EndPageNumber)
	case "content_block_location":
		putIndex("document_index", c.DocumentIndex)
		putIndex("start_block_index", c.StartBlockIndex)
		putIndex("end_block_index", c.EndBlockIndex)
	case "search_result_location":
		putIndex("search_result_index", c.SearchResultIndex)
		putIndex("start_block_index", c.StartBlockIndex)
		putIndex("end_block_index", c.EndBlockIndex)
	}
	return json.Marshal(fields)
}
