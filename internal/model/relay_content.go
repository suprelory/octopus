package model

// RelayMessage describes application-level bytes observed at one transport boundary.
// Data is gzip-compressed and is never included in list/detail JSON responses.
// Body is populated only by the explicit content endpoint.
type RelayMessage struct {
	Method        string              `json:"method,omitempty"`
	URL           string              `json:"url,omitempty"`
	StatusCode    *int                `json:"status_code,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	ContentType   string              `json:"content_type,omitempty"`
	State         string              `json:"state"`
	Reason        string              `json:"reason,omitempty"`
	Bytes         int64               `json:"bytes"`
	CapturedBytes int64               `json:"captured_bytes"`
	Body          string              `json:"body,omitempty"`
	BodyEncoding  string              `json:"body_encoding,omitempty"`
	Data          []byte              `json:"-"`
}

type RelayExchange struct {
	AttemptID         string        `json:"attempt_id,omitempty"`
	ChannelID         int           `json:"channel_id,omitempty"`
	ChannelName       string        `json:"channel_name,omitempty"`
	Model             string        `json:"model,omitempty"`
	Transport         string        `json:"transport"`
	UpstreamRequestID string        `json:"upstream_request_id,omitempty"`
	Error             string        `json:"error,omitempty"`
	Request           *RelayMessage `json:"request,omitempty"`
	Response          *RelayMessage `json:"response,omitempty"`
}

type RelayTrace struct {
	ID       string          `json:"id"`
	Client   RelayExchange   `json:"client"`
	Attempts []RelayExchange `json:"attempts"`
}

// Body columns live outside relay_logs so list queries, retention and live
// subscribers do not carry large payloads. Metadata survives body expiration.
type RelayRequestContent struct {
	BodiesExpired bool
	LogID         int64  `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt     int64  `gorm:"index"`
	Metadata      string `gorm:"type:text"`
	RequestBody   []byte
	ResponseBody  []byte
}

type RelayAttemptContent struct {
	BodiesExpired bool
	LogID         int64  `gorm:"primaryKey;autoIncrement:false"`
	AttemptID     string `gorm:"primaryKey;size:64"`
	CreatedAt     int64  `gorm:"index"`
	Metadata      string `gorm:"type:text"`
	RequestBody   []byte
	ResponseBody  []byte
}
