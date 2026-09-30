package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProtocolErrorResponse contains both response forms because an early SSE
// heartbeat may commit stream headers before relay selects the final error.
type ProtocolErrorResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	StreamBody []byte
}

// NormalizeHTTPError extracts common OpenAI, Anthropic, Gemini, and compatible
// error envelopes without discarding the upstream status or request ID.
func NormalizeHTTPError(statusCode int, headers http.Header, body []byte, defaultType string) *ResponseError {
	if statusCode < 400 || statusCode > 599 {
		statusCode = http.StatusBadGateway
	}
	detail := ErrorDetail{Type: defaultType}

	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) == nil {
		parseTopLevelErrorFields(envelope, &detail)
		if raw, ok := envelope["error"]; ok {
			parseErrorDetail(raw, &detail)
		}
	}

	if detail.Message == "" {
		detail.Message = strings.TrimSpace(string(bytes.TrimSpace(body)))
		if len(detail.Message) > 4096 {
			detail.Message = detail.Message[:4096]
		}
	}
	if detail.Message == "" {
		detail.Message = http.StatusText(statusCode)
	}
	if detail.Type == "" {
		detail.Type = "upstream_error"
	}
	if detail.RequestID == "" && headers != nil {
		for _, key := range []string{"request-id", "x-request-id", "x-goog-request-id"} {
			if value := strings.TrimSpace(headers.Get(key)); value != "" {
				detail.RequestID = value
				break
			}
		}
	}

	return &ResponseError{StatusCode: statusCode, Detail: detail}
}

func parseErrorDetail(raw json.RawMessage, detail *ErrorDetail) {
	var message string
	if json.Unmarshal(raw, &message) == nil {
		detail.Message = message
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	nested := ErrorDetail{}
	parseTopLevelErrorFields(fields, &nested)
	if nested.Message != "" {
		detail.Message = nested.Message
	}
	if nested.Type != "" {
		detail.Type = nested.Type
	}
	if nested.Code != "" {
		detail.Code = nested.Code
	}
	if nested.Param != "" {
		detail.Param = nested.Param
	}
	if nested.RequestID != "" {
		detail.RequestID = nested.RequestID
	}
}

func parseTopLevelErrorFields(fields map[string]json.RawMessage, detail *ErrorDetail) {
	if detail.Message == "" {
		detail.Message = rawString(fields["message"])
	}
	if value := rawString(fields["type"]); value != "" {
		detail.Type = value
	} else if value := rawString(fields["status"]); value != "" {
		detail.Type = value
	}
	if value := rawScalarString(fields["code"]); value != "" {
		detail.Code = value
	}
	if value := rawScalarString(fields["param"]); value != "" && value != "null" {
		detail.Param = value
	}
	if value := rawString(fields["request_id"]); value != "" {
		detail.RequestID = value
	}
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return ""
}

func rawScalarString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if value := rawString(raw); value != "" {
		return value
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func NormalizeResponseError(response *ResponseError, fallbackStatus int, fallbackType string) *ResponseError {
	if fallbackStatus < 400 || fallbackStatus > 599 {
		fallbackStatus = http.StatusBadGateway
	}
	if response == nil {
		return &ResponseError{
			StatusCode: fallbackStatus,
			Detail:     ErrorDetail{Type: fallbackType, Message: http.StatusText(fallbackStatus)},
		}
	}
	copy := *response
	if copy.StatusCode < 400 || copy.StatusCode > 599 {
		copy.StatusCode = fallbackStatus
	}
	if copy.Detail.Type == "" {
		copy.Detail.Type = fallbackType
	}
	if copy.Detail.Message == "" {
		copy.Detail.Message = http.StatusText(copy.StatusCode)
	}
	return &copy
}
