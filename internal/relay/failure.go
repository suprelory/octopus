package relay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// FailureClass is the relay's stable, transport-independent error taxonomy.
// The class is deliberately narrower than an HTTP status code: status codes
// describe a response, while this value controls retry and circuit behaviour.
type FailureClass string

const (
	FailureNone             FailureClass = "none"
	FailureRequest          FailureClass = "request"
	FailureConfiguration    FailureClass = "configuration"
	FailureAuthentication   FailureClass = "authentication"
	FailurePermission       FailureClass = "permission"
	FailureQuota            FailureClass = "quota"
	FailureRateLimit        FailureClass = "rate_limit"
	FailureModelUnsupported FailureClass = "model_unsupported"
	FailureTransient        FailureClass = "transient"
	FailureClientCanceled   FailureClass = "client_canceled"
)

// FailureClassification contains the decision made from one upstream error.
// RetryAt is absolute so a retry remains correct after queueing or failover.
type FailureClassification struct {
	Class       FailureClass
	Retryable   bool
	Record      bool
	Passthrough bool
	RetryAt     time.Time
	StatusCode  int
}

// classifiedRelayError preserves the source category across fmt.Errorf
// wrapping. It is used for local faults (request construction and overrides),
// where no upstream HTTP status exists to classify.
type classifiedRelayError struct {
	class FailureClass
	err   error
}

func (e *classifiedRelayError) Error() string {
	if e == nil || e.err == nil {
		return string(e.class)
	}
	return e.err.Error()
}

func (e *classifiedRelayError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func classifyLocalRelayError(class FailureClass, err error) error {
	if err == nil {
		return nil
	}
	return &classifiedRelayError{class: class, err: err}
}

func localRelayErrorClass(err error) (FailureClass, bool) {
	var classified *classifiedRelayError
	if !errors.As(err, &classified) || classified == nil {
		return FailureNone, false
	}
	return classified.class, true
}

// classifyRelayFailure turns a status/error pair into retry and breaker
// behaviour. Error envelopes are checked before status codes because providers
// commonly return quota or model errors with a generic 400/403/500 status.
func classifyRelayFailure(statusCode int, err error, retryAt time.Time) FailureClassification {
	return classifyRelayFailureContext(nil, statusCode, err, retryAt)
}

// ClassifyRelayFailure exposes the same taxonomy to package-level diagnostics
// and tests without requiring callers to know the internal context variant.
func ClassifyRelayFailure(statusCode int, err error, retryAt time.Time) FailureClassification {
	return classifyRelayFailure(statusCode, err, retryAt)
}

func classifyRelayFailureContext(ctx context.Context, statusCode int, err error, retryAt time.Time) FailureClassification {
	if ctx != nil && ctx.Err() != nil && isClientCancellation(ctx, err) {
		return FailureClassification{Class: FailureClientCanceled, StatusCode: statusCode, RetryAt: retryAt}
	}
	if localClass, ok := localRelayErrorClass(err); ok {
		result := FailureClassification{Class: localClass, StatusCode: statusCode, RetryAt: retryAt}
		if localClass == FailureTransient {
			result.Retryable = true
			result.Record = true
		}
		return result
	}

	code, typ, message := responseErrorFields(err)
	if statusCode <= 0 {
		var responseError *transformerModel.ResponseError
		if errors.As(err, &responseError) && responseError != nil && responseError.StatusCode > 0 {
			statusCode = responseError.StatusCode
		}
	}
	text := strings.ToLower(strings.Join([]string{code, typ, message, relayErrorMessage(err)}, " "))

	// A context error which is not a downstream cancellation (for example an
	// upstream deadline) is transient. The caller still handles client context
	// cancellation separately before reaching this function.
	if errors.Is(err, context.Canceled) && (ctx == nil || ctx.Err() != nil) {
		return FailureClassification{Class: FailureClientCanceled, StatusCode: statusCode, RetryAt: retryAt}
	}

	if isQuotaText(text) {
		return FailureClassification{
			Class:       FailureQuota,
			Retryable:   true,
			Record:      true,
			Passthrough: failurePassthroughStatus(statusCode),
			RetryAt:     retryAt,
			StatusCode:  statusCode,
		}
	}
	if isRateLimitText(text) || statusCode == http.StatusTooManyRequests || statusCode == 529 {
		return FailureClassification{
			Class:       FailureRateLimit,
			Retryable:   true,
			Record:      true,
			Passthrough: failurePassthroughStatus(statusCode),
			RetryAt:     retryAt,
			StatusCode:  statusCode,
		}
	}
	if isModelUnsupportedText(text) {
		return FailureClassification{
			Class:      FailureModelUnsupported,
			Record:     true,
			RetryAt:    retryAt,
			StatusCode: statusCode,
		}
	}
	if isAuthenticationText(text) {
		return FailureClassification{Class: FailureAuthentication, Record: true, RetryAt: retryAt, StatusCode: statusCode}
	}
	if isPermissionText(text) {
		return FailureClassification{Class: FailurePermission, Record: true, RetryAt: retryAt, StatusCode: statusCode}
	}
	if statusCode == http.StatusUnauthorized {
		return FailureClassification{Class: FailureAuthentication, Record: true, RetryAt: retryAt, StatusCode: statusCode}
	}
	if statusCode == http.StatusForbidden {
		return FailureClassification{Class: FailurePermission, Record: true, RetryAt: retryAt, StatusCode: statusCode}
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooEarly {
		return FailureClassification{Class: FailureTransient, Retryable: true, Record: true, RetryAt: retryAt, StatusCode: statusCode}
	}
	if statusCode == http.StatusNotFound {
		// A bare 404 is commonly an endpoint/route mismatch. Only suspend a
		// model when the provider explicitly identifies the model as missing.
		return FailureClassification{Class: FailureRequest, RetryAt: retryAt, StatusCode: statusCode}
	}
	if statusCode >= 400 && statusCode < 500 {
		return FailureClassification{Class: FailureRequest, RetryAt: retryAt, StatusCode: statusCode}
	}

	// Status 0 is the normal shape of a network/DNS/TLS error. A 5xx response
	// and malformed upstream response are also transient provider failures.
	if statusCode == 0 || statusCode >= 500 || err != nil {
		return FailureClassification{
			Class:       FailureTransient,
			Retryable:   true,
			Record:      true,
			Passthrough: statusCode == http.StatusServiceUnavailable || statusCode == http.StatusBadGateway || statusCode == http.StatusGatewayTimeout,
			RetryAt:     retryAt,
			StatusCode:  statusCode,
		}
	}
	if statusCode >= 200 && statusCode < 400 && err == nil {
		return FailureClassification{Class: FailureNone, StatusCode: statusCode, RetryAt: retryAt}
	}
	return FailureClassification{Class: FailureRequest, RetryAt: retryAt, StatusCode: statusCode}
}

func failurePassthroughStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable ||
		statusCode == http.StatusGatewayTimeout ||
		statusCode == 529
}

func responseErrorFields(err error) (code, typ, message string) {
	var responseError *transformerModel.ResponseError
	if !errors.As(err, &responseError) || responseError == nil {
		return "", "", ""
	}
	return responseError.Detail.Code, responseError.Detail.Type, responseError.Detail.Message
}

func isQuotaText(text string) bool {
	return strings.Contains(text, "insufficient_quota") ||
		strings.Contains(text, "quota exceeded") ||
		strings.Contains(text, "quota_exceeded") ||
		strings.Contains(text, "no available account") ||
		(strings.Contains(text, "billing") && strings.Contains(text, "hard limit")) ||
		strings.Contains(text, "exceeded your current quota")
}

func isRateLimitText(text string) bool {
	return strings.Contains(text, "rate_limit_exceeded") ||
		strings.Contains(text, "rate_limit") ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "too many requests") ||
		strings.Contains(text, "resource exhausted") ||
		strings.Contains(text, "overloaded")
}

func isModelUnsupportedText(text string) bool {
	return strings.Contains(text, "model_not_found") ||
		strings.Contains(text, "model_not_supported") ||
		strings.Contains(text, "model_unsupported") ||
		strings.Contains(text, "invalid_model") ||
		strings.Contains(text, "model not found") ||
		strings.Contains(text, "model does not exist") ||
		strings.Contains(text, "model is not available") ||
		strings.Contains(text, "unsupported model") ||
		strings.Contains(text, "does not support model")
}

func isAuthenticationText(text string) bool {
	return strings.Contains(text, "invalid_api_key") ||
		strings.Contains(text, "invalid api key") ||
		strings.Contains(text, "api_key_invalid") ||
		strings.Contains(text, "authentication") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "api key is invalid")
}

func isPermissionText(text string) bool {
	return strings.Contains(text, "permission_denied") ||
		strings.Contains(text, "access_denied") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "does not have permission") ||
		strings.Contains(text, "not authorized")
}

func failureCircuitKind(classification FailureClassification) balancer.FailureKind {
	switch classification.Class {
	case FailureRequest, FailureConfiguration, FailureClientCanceled, FailureNone:
		return balancer.FailureIgnored
	case FailureRateLimit:
		return balancer.FailureRateLimit
	case FailureQuota:
		return balancer.FailureQuota
	case FailureModelUnsupported:
		return balancer.FailureModelUnsupported
	case FailureAuthentication:
		return balancer.FailureAuthentication
	case FailurePermission:
		return balancer.FailurePermission
	default:
		return balancer.FailureTransient
	}
}

func recordFailureAndResolveRetryAt(channelID, keyID int, modelName string, classification FailureClassification, retryAt time.Time) time.Time {
	balancer.RecordFailureAt(channelID, keyID, modelName, failureCircuitKind(classification), retryAt)
	if deadline, ok := balancer.RetryAt(channelID, keyID, modelName); ok {
		return deadline
	}
	return retryAt
}
