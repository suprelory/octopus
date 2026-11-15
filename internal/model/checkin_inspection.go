package model

import "time"

// Account observations are separate from the daily check-in ledger. A failed
// read keeps the last balance snapshot, with its original observation time.
type CheckinBalanceSnapshot struct {
	AmountMicros *int64    `json:"amount_micros,omitempty,string"`
	UsedMicros   *int64    `json:"used_micros,omitempty,string"`
	AmountState  string    `json:"amount_state"`
	UsedState    string    `json:"used_state"`
	Unit         string    `json:"unit"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Only the latest inspection and its bounded, redacted request stages are kept.
// Neither an inspection nor a balance refresh creates a CheckinRun or Job.
type CheckinInspection struct {
	ID             string                  `json:"id"`
	Kind           string                  `json:"kind"`
	Status         string                  `json:"status"`
	Reason         string                  `json:"reason"`
	Authentication string                  `json:"authentication"`
	UserID         string                  `json:"user_id,omitempty"`
	BalanceUpdated bool                    `json:"balance_updated"`
	CheckinSupport string                  `json:"checkin_support"`
	CheckinStatus  string                  `json:"checkin_status"`
	CheckinDate    string                  `json:"checkin_date"`
	Timezone       string                  `json:"timezone"`
	StartedAt      time.Time               `json:"started_at"`
	FinishedAt     *time.Time              `json:"finished_at,omitempty"`
	Steps          []CheckinInspectionStep `json:"steps"`
}

type CheckinInspectionStep struct {
	Stage      string    `json:"stage"`
	HTTPStatus int       `json:"http_status"`
	Reason     string    `json:"reason"`
	DurationMS int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}
