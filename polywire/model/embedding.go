package model

import (
	"encoding/json"
	"errors"
)

// EmbeddingInput represents the input for embedding requests.
// It can be a single string or an array of strings.
type EmbeddingInput struct {
	Single   *string
	Multiple []string
}

func (i EmbeddingInput) MarshalJSON() ([]byte, error) {
	if i.Single != nil {
		return json.Marshal(i.Single)
	}

	if len(i.Multiple) > 0 {
		return json.Marshal(i.Multiple)
	}

	return []byte("null"), nil
}

func (i *EmbeddingInput) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		i.Single = &str
		return nil
	}

	var strs []string

	err = json.Unmarshal(data, &strs)
	if err == nil {
		i.Multiple = strs
		return nil
	}

	return errors.New("invalid input type")
}

// EmbeddingObject represents a single embedding object in the response.
type EmbeddingObject struct {
	// The object type, always "embedding".
	Object string `json:"object"`
	// The index of this embedding in the list.
	Index int `json:"index"`
	// The embedding vector.
	Embedding Embedding `json:"embedding"`
}

// Embedding represents an embedding vector.
// It can be a float array or a base64-encoded string.
type Embedding struct {
	FloatArray   []float64
	Base64String *string
}

func (e Embedding) MarshalJSON() ([]byte, error) {
	if e.Base64String != nil {
		return json.Marshal(e.Base64String)
	}

	if len(e.FloatArray) > 0 {
		return json.Marshal(e.FloatArray)
	}

	return []byte("[]"), nil
}

func (e *Embedding) UnmarshalJSON(data []byte) error {
	var str string

	err := json.Unmarshal(data, &str)
	if err == nil {
		e.Base64String = &str
		return nil
	}

	var floats []float64

	err = json.Unmarshal(data, &floats)
	if err == nil {
		e.FloatArray = floats
		return nil
	}

	return errors.New("invalid embedding type")
}
