package model

import "time"

const (
	SiteCheckinReasonCheckedIn        = "checked_in"
	SiteCheckinReasonAlreadyCheckedIn = "already_checked_in"
	SiteCheckinReasonAlreadyRunning   = "already_running"
	SiteCheckinReasonUnsupported      = "unsupported_checkin"
)

// SiteCheckinLog is an immutable outcome, separate from the account's latest
// status. Names are snapshots so history remains readable after account edits.
// No credential or upstream response body belongs in this table.
type SiteCheckinLog struct {
	ID          int64               `json:"id" gorm:"primaryKey;autoIncrement:false;index:idx_site_checkin_site_id,priority:2,sort:desc;index:idx_site_checkin_account_id,priority:2,sort:desc"`
	SiteID      int                 `json:"site_id" gorm:"index:idx_site_checkin_site_id,priority:1;not null"`
	AccountID   int                 `json:"account_id" gorm:"index:idx_site_checkin_account_id,priority:1;not null"`
	SiteName    string              `json:"site_name"`
	AccountName string              `json:"account_name"`
	Platform    SitePlatform        `json:"platform" gorm:"size:32;not null"`
	Source      string              `json:"source" gorm:"size:16;not null"`
	Status      SiteExecutionStatus `json:"status" gorm:"size:16;not null"`
	Reason      string              `json:"reason" gorm:"size:64;not null"`
	Message     string              `json:"message"`
	Reward      string              `json:"reward"`
	DurationMs  int64               `json:"duration_ms"`
	StartedAt   time.Time           `json:"started_at"`
	FinishedAt  time.Time           `json:"finished_at" gorm:"index"`
}

type SiteCheckinLogPage struct {
	Items        []SiteCheckinLog `json:"items"`
	NextBeforeID int64            `json:"next_before_id,omitempty"`
}
