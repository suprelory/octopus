package relay

const (
	CodeRelayModelNotSupported     = "relay.model_not_supported"
	CodeRelayCapabilityRejected    = "relay.capability_rejected"
	CodeRelayModelNotFound         = "relay.model_not_found"
	CodeRelayNoAvailableChannel    = "relay.no_available_channel"
	CodeRelayChannelDisabled       = "relay.channel_disabled"
	CodeRelayNoAvailableKey        = "relay.no_available_key"
	CodeRelayUpstreamFailed        = "relay.upstream_failed"
	CodeRelayConfiguration         = "relay.configuration_error"
	CodeRelayInvalidRequest        = "relay.invalid_request"
	CodeRelayAuthentication        = "relay.authentication_failed"
	CodeRelayPermission            = "relay.permission_denied"
	CodeRelayQuota                 = "relay.quota_exceeded"
	CodeRelayRateLimit             = "relay.rate_limited"
	CodeRelayRequestTooLarge       = "relay.request_too_large"
	CodeRelayTimeout               = "relay.timeout"
	CodeRelayCircuitBreakerTripped = "relay.circuit_breaker_tripped"
)
