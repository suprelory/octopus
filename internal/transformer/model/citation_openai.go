package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func OpenAICitationFromRaw(raw json.RawMessage, format APIFormat) (Citation, error) {
	var wire struct {
		Type        string          `json:"type"`
		URL         string          `json:"url"`
		Title       string          `json:"title"`
		StartIndex  int             `json:"start_index"`
		EndIndex    int             `json:"end_index"`
		FileID      string          `json:"file_id"`
		Filename    string          `json:"filename"`
		ContainerID string          `json:"container_id"`
		Index       *int            `json:"index"`
		URLCitation json.RawMessage `json:"url_citation"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Citation{}, fmt.Errorf("invalid OpenAI citation: %w", err)
	}
	kind := wire.Type
	if format == APIFormatOpenAIChatCompletion && len(wire.URLCitation) > 0 {
		if err := json.Unmarshal(wire.URLCitation, &wire); err != nil {
			return Citation{}, err
		}
	}
	return Citation{Provider: string(SignatureProviderOpenAI), Format: format, Raw: bytes.Clone(raw), Type: kind,
		URI: wire.URL, Title: wire.Title, StartIndex: wire.StartIndex, EndIndex: wire.EndIndex,
		FileID: wire.FileID, Filename: wire.Filename, ContainerID: wire.ContainerID, AnnotationIndex: wire.Index}, nil
}

func OpenAICitationForWire(citation Citation, format APIFormat) (json.RawMessage, error) {
	if citation.Provider == string(SignatureProviderOpenAI) && citation.Format == format && len(citation.Raw) > 0 {
		return bytes.Clone(citation.Raw), nil
	}
	if citation.URI == "" {
		return nil, fmt.Errorf("citation %q has no representation in %s", citation.Type, format)
	}
	fields := map[string]any{"url": citation.URI, "title": citation.Title, "start_index": citation.StartIndex, "end_index": citation.EndIndex}
	if format == APIFormatOpenAIChatCompletion {
		return json.Marshal(map[string]any{"type": "url_citation", "url_citation": fields})
	}
	fields["type"] = "url_citation"
	return json.Marshal(fields)
}
