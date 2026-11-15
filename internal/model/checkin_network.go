package model

const (
	CheckinNetworkError             = "network_error"
	CheckinNetworkDNSError          = "network_dns_error"
	CheckinNetworkProxyError        = "network_proxy_error"
	CheckinNetworkTLSError          = "network_tls_error"
	CheckinNetworkTimeout           = "network_timeout"
	CheckinNetworkConnectionRefused = "network_connection_refused"
	CheckinNetworkConnectionReset   = "network_connection_reset"
	CheckinNetworkConnectionError   = "network_connection_error"
	CheckinNetworkResponseReadError = "network_response_read_error"
	CheckinNetworkResponseTooLarge  = "network_response_too_large"
)

func IsCheckinNetworkReason(reason string) bool {
	switch reason {
	case CheckinNetworkError,
		CheckinNetworkDNSError,
		CheckinNetworkProxyError,
		CheckinNetworkTLSError,
		CheckinNetworkTimeout,
		CheckinNetworkConnectionRefused,
		CheckinNetworkConnectionReset,
		CheckinNetworkConnectionError,
		CheckinNetworkResponseReadError,
		CheckinNetworkResponseTooLarge:
		return true
	default:
		return false
	}
}
