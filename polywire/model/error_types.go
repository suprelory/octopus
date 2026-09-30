package model

import (
	"fmt"
	"net/http"
	"strings"
)

// ResponseError represents an error response.
type ResponseError struct {
	StatusCode int         `json:"-"`
	Detail     ErrorDetail `json:"error"`
}

func (e ResponseError) Error() string {
	sb := strings.Builder{}
	if e.StatusCode != 0 {
		sb.WriteString(fmt.Sprintf("Request failed: %s, ", http.StatusText(e.StatusCode)))
	}

	if e.Detail.Message != "" {
		sb.WriteString("error: ")
		sb.WriteString(e.Detail.Message)
	}

	if e.Detail.Code != "" {
		sb.WriteString(", code: ")
		sb.WriteString(e.Detail.Code)
	}

	if e.Detail.Type != "" {
		sb.WriteString(", type: ")
		sb.WriteString(e.Detail.Type)
	}

	if e.Detail.RequestID != "" {
		sb.WriteString(", request_id: ")
		sb.WriteString(e.Detail.RequestID)
	}

	return sb.String()
}

// ErrorDetail represents error details.
type ErrorDetail struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Type      string `json:"type"`
	Param     string `json:"param,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}
