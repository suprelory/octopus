package model

import (
	"encoding/json"
	"time"
)

// Check-in records deliberately have no channel, group or subscription references.
type CheckinHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CheckinRequest struct {
	Method  string          `json:"method"`
	Path    string          `json:"path"`
	Body    string          `json:"body"`
	Headers []CheckinHeader `json:"headers"`
}

type CheckinMatch struct {
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type CheckinStatusRule struct {
	Request   CheckinRequest `json:"request"`
	Checked   CheckinMatch   `json:"checked"`
	Unchecked CheckinMatch   `json:"unchecked"`
	DatePath  string         `json:"date_path"`
}

type CheckinRewardRule struct {
	Source     string `json:"source"`
	Path       string `json:"path"`
	Unit       string `json:"unit"`
	Multiplier string `json:"multiplier"`
}

type CheckinSiteConfig struct {
	Name          string                `json:"name"`
	BaseURL       string                `json:"base_url"`
	Adapter       string                `json:"adapter"`
	Enabled       bool                  `json:"enabled"`
	Timezone      string                `json:"timezone"`
	WindowStart   string                `json:"window_start"`
	WindowEnd     string                `json:"window_end"`
	JitterMinutes int                   `json:"jitter_minutes"`
	ProxyURL      string                `json:"proxy_url"`
	ProxyMode     string                `json:"proxy_mode,omitempty"`
	ProxyConfigID *int                  `json:"proxy_config_id,omitempty"`
	ManualURL     string                `json:"manual_url"`
	UserAgent     string                `json:"user_agent,omitempty"`
	Turnstile     *CheckinTurnstileSite `json:"turnstile,omitempty"`
	Submit        CheckinRequest        `json:"submit"`
	Success       *CheckinMatch         `json:"success,omitempty"`
	Already       *CheckinMatch         `json:"already,omitempty"`
	Status        *CheckinStatusRule    `json:"status,omitempty"`
	StatusMode    string                `json:"status_mode,omitempty"`
	Reward        CheckinRewardRule     `json:"reward"`
}

type CheckinSite struct {
	ID     string            `json:"id" gorm:"type:varchar(36);primaryKey"`
	Config CheckinSiteConfig `json:"config" gorm:"type:text;serializer:json"`
	// Indexed projection of Config.ProxyConfigID, maintained at every write boundary.
	ProxyConfigID      *int                `json:"-" gorm:"index"`
	ProxyConfiguration *ProxyConfiguration `json:"-" gorm:"foreignKey:ProxyConfigID;constraint:OnDelete:RESTRICT"`
	Revision           int64               `json:"revision"`
	Support            string              `json:"support"`
	SupportRevision    int64               `json:"support_revision"`
	VerifiedAt         *time.Time          `json:"verified_at,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	DeletedAt          *time.Time          `json:"deleted_at,omitempty" gorm:"index"`
}

type CheckinCredentials struct {
	Type         string               `json:"type"`
	Cookie       string               `json:"cookie,omitempty"`
	AccessToken  string               `json:"access_token,omitempty"`
	UserID       string               `json:"user_id,omitempty"`
	RefreshToken string               `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time           `json:"expires_at,omitempty"`
	Username     string               `json:"username,omitempty"`
	Password     string               `json:"password,omitempty"`
	Session      *CheckinLoginSession `json:"session,omitempty"`
}

// Derived login material is encrypted with the original credentials. Keeping
// cookie scope and expiry avoids widening cookies when a worker restarts.
type CheckinSessionCookie struct {
	Name    string    `json:"name"`
	Value   string    `json:"value"`
	Domain  string    `json:"domain,omitempty"`
	Path    string    `json:"path"`
	Secure  bool      `json:"secure,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
}

type CheckinLoginSession struct {
	BaseURL     string                 `json:"base_url"`
	Auth        string                 `json:"auth"`
	AccessToken string                 `json:"access_token,omitempty"`
	Cookies     []CheckinSessionCookie `json:"cookies"`
}

type CheckinAccount struct {
	ID                        string              `json:"id" gorm:"type:varchar(36);primaryKey"`
	SiteID                    string              `json:"site_id" gorm:"type:varchar(36);not null;index"`
	Site                      *CheckinSite        `json:"-" gorm:"foreignKey:SiteID;constraint:OnDelete:RESTRICT"`
	Name                      string              `json:"name"`
	Enabled                   bool                `json:"enabled"`
	AutoCheckin               bool                `json:"auto_checkin"`
	ProxyMode                 string              `json:"proxy_mode"`
	ProxyURL                  string              `json:"proxy_url"`
	ProxyConfigID             *int                `json:"proxy_config_id,omitempty" gorm:"index"`
	ProxyConfiguration        *ProxyConfiguration `json:"-" gorm:"foreignKey:ProxyConfigID;constraint:OnDelete:RESTRICT"`
	CredentialType            string              `json:"credential_type"`
	CredentialUserID          string              `json:"user_id" gorm:"size:64"`
	CredentialCipher          string              `json:"-" gorm:"type:text"`
	CredentialVersion         int64               `json:"credential_version"`
	CredentialIdentityVersion int64               `json:"-"`
	HasRefreshToken           bool                `json:"has_refresh_token"`
	TokenExpiresAt            *time.Time          `json:"token_expires_at,omitempty"`
	TokenRefreshedAt          *time.Time          `json:"token_refreshed_at,omitempty"`
	RefreshPending            bool                `json:"refresh_pending,omitempty"`
	RefreshError              string              `json:"refresh_error,omitempty" gorm:"size:64"`
	RefreshRetryAt            *time.Time          `json:"refresh_retry_at,omitempty"`
	HasSession                bool                `json:"has_session"`
	SessionSavedAt            *time.Time          `json:"session_saved_at,omitempty"`
	Revision                  int64               `json:"revision"`
	NextRunAt                 *time.Time          `json:"next_run_at,omitempty" gorm:"index"`
	LastRunID                 string              `json:"last_run_id" gorm:"type:varchar(36)"`
	LastStatus                string              `json:"last_status"`
	LastSuccessDate           string              `json:"last_success_date" gorm:"size:10"`
	FailureCount              int                 `json:"failure_count"`
	LeaseOwner                string              `json:"-" gorm:"size:36"`
	LeaseUntil                *time.Time          `json:"-"`
	CreatedAt                 time.Time           `json:"created_at"`
	UpdatedAt                 time.Time           `json:"updated_at"`
	DeletedAt                 *time.Time          `json:"deleted_at,omitempty" gorm:"index"`

	Balance    *CheckinBalanceSnapshot `json:"balance,omitempty" gorm:"type:text;serializer:json"`
	Inspection *CheckinInspection      `json:"inspection,omitempty" gorm:"type:text;serializer:json"`
}

// One run is the durable ledger for one account and one business date. Attempts
// can be retained separately; polling and retries never create another ledger.
type CheckinRun struct {
	ID               string          `json:"id" gorm:"type:varchar(36);primaryKey"`
	SiteID           string          `json:"site_id" gorm:"type:varchar(36);index"`
	AccountID        string          `json:"account_id" gorm:"type:varchar(36);uniqueIndex:idx_checkin_account_date,priority:1"`
	Account          *CheckinAccount `json:"-" gorm:"foreignKey:AccountID;constraint:OnDelete:RESTRICT"`
	CheckinDate      string          `json:"checkin_date" gorm:"size:10;uniqueIndex:idx_checkin_account_date,priority:2;index"`
	SiteName         string          `json:"site_name"`
	AccountName      string          `json:"account_name"`
	Timezone         string          `json:"timezone"`
	Fingerprint      string          `json:"fingerprint" gorm:"size:64"`
	ProxyFingerprint string          `json:"proxy_fingerprint,omitempty" gorm:"size:64"`
	Phase            string          `json:"phase"`
	Status           string          `json:"status" gorm:"size:32;index"`
	Reason           string          `json:"reason" gorm:"size:64"`
	Source           string          `json:"source"`
	SubmitIntentAt   *time.Time      `json:"submit_intent_at,omitempty"`
	BeforeUnchecked  bool            `json:"before_unchecked"`
	SafeToRetry      bool            `json:"safe_to_retry"`
	NextConfirmAt    *time.Time      `json:"next_confirm_at,omitempty" gorm:"index"`
	ConfirmDeadline  *time.Time      `json:"confirm_deadline,omitempty"`
	RewardMicros     *int64          `json:"reward_micros,omitempty,string"`
	RewardUnit       string          `json:"reward_unit"`
	RewardState      string          `json:"reward_state"`
	DetailsCleanedAt *time.Time      `json:"details_cleaned_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type CheckinAttempt struct {
	ID         string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	RunID      string      `json:"run_id" gorm:"type:varchar(36);index"`
	Run        *CheckinRun `json:"-" gorm:"foreignKey:RunID;constraint:OnDelete:RESTRICT"`
	JobID      string      `json:"job_id" gorm:"type:varchar(36);index"`
	Stage      string      `json:"stage"`
	HTTPStatus int         `json:"http_status"`
	Reason     string      `json:"reason"`
	Message    string      `json:"message"`
	DurationMS int64       `json:"duration_ms"`
	CreatedAt  time.Time   `json:"created_at" gorm:"index"`
}

// Automatic token rotation keeps the same identity. Old accounts use their
// existing credential version until the first rotation or manual replacement.
func (a CheckinAccount) IdentityVersion() int64 {
	if a.CredentialIdentityVersion > 0 {
		return a.CredentialIdentityVersion
	}
	return a.CredentialVersion
}

type CheckinScope struct {
	Kind     string   `json:"kind"`
	IDs      []string `json:"ids"`
	OnlyAuto bool     `json:"only_auto"`
}

type CheckinJob struct {
	ID             string       `json:"id" gorm:"type:varchar(36);primaryKey"`
	IdempotencyKey string       `json:"-" gorm:"size:128;uniqueIndex"`
	ScopeHash      string       `json:"-" gorm:"size:64;index"`
	Scope          CheckinScope `json:"scope" gorm:"type:text;serializer:json"`
	Source         string       `json:"source"`
	Status         string       `json:"status" gorm:"size:32;index"`
	Total          int          `json:"total"`
	Completed      int          `json:"completed"`
	Success        int          `json:"success"`
	Already        int          `json:"already"`
	Failed         int          `json:"failed"`
	Pending        int          `json:"pending"`
	Skipped        int          `json:"skipped"`
	CreatedAt      time.Time    `json:"created_at" gorm:"index"`
	UpdatedAt      time.Time    `json:"updated_at"`
	FinishedAt     *time.Time   `json:"finished_at,omitempty" gorm:"index"`
}

type CheckinJobItem struct {
	ID          string          `json:"id" gorm:"type:varchar(36);primaryKey"`
	JobID       string          `json:"job_id" gorm:"type:varchar(36);uniqueIndex:idx_checkin_job_account,priority:1;index"`
	AccountID   string          `json:"account_id" gorm:"type:varchar(36);uniqueIndex:idx_checkin_job_account,priority:2;index:idx_checkin_item_account_date,priority:1"`
	SiteID      string          `json:"site_id" gorm:"type:varchar(36);index"`
	CheckinDate string          `json:"checkin_date" gorm:"size:10;uniqueIndex:idx_checkin_job_account,priority:3;index:idx_checkin_item_account_date,priority:2"`
	RunID       string          `json:"run_id" gorm:"type:varchar(36);index"`
	SiteName    string          `json:"site_name"`
	AccountName string          `json:"account_name"`
	Job         *CheckinJob     `json:"-" gorm:"foreignKey:JobID;constraint:OnDelete:RESTRICT"`
	Account     *CheckinAccount `json:"-" gorm:"foreignKey:AccountID;constraint:OnDelete:RESTRICT"`
	ConfirmOnly bool            `json:"confirm_only"`
	State       string          `json:"state" gorm:"size:16;index"`
	Outcome     string          `json:"outcome"`
	LeaseOwner  string          `json:"-" gorm:"size:36"`
	LeaseUntil  *time.Time      `json:"-"`
	CreatedAt   time.Time       `json:"created_at" gorm:"index"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type CheckinSettings struct {
	AutoCleanup            bool `json:"auto_cleanup"`
	RetentionDays          int  `json:"retention_days"`
	Concurrency            int  `json:"concurrency"`
	SiteIntervalSeconds    int  `json:"site_interval_seconds"`
	HTTPTimeoutSeconds     int  `json:"http_timeout_seconds"`
	ConfirmAttempts        int  `json:"confirm_attempts"`
	ConfirmIntervalSeconds int  `json:"confirm_interval_seconds"`
	ConfirmTimeoutMinutes  int  `json:"confirm_timeout_minutes"`
}

func DefaultCheckinSettings() CheckinSettings {
	return CheckinSettings{Concurrency: 4, SiteIntervalSeconds: 2, HTTPTimeoutSeconds: 30, ConfirmAttempts: 3, ConfirmIntervalSeconds: 2, ConfirmTimeoutMinutes: 60, RetentionDays: 90}
}

// Normal API serialization never includes ciphertext. Export uses an explicit
// envelope; its key is intentionally stored separately from the database dump.
type CheckinAccountBackup struct {
	CheckinAccount
	Cipher          string `json:"credential_cipher"`
	IdentityVersion int64  `json:"credential_identity_version,omitempty"`
}

type CheckinDump struct {
	Version  int                    `json:"version"`
	Sites    []CheckinSite          `json:"sites"`
	Accounts []CheckinAccountBackup `json:"accounts"`
	Runs     []CheckinRun           `json:"runs"`
	Attempts []CheckinAttempt       `json:"attempts"`
	Jobs     []CheckinJob           `json:"jobs"`
	Items    []CheckinJobItem       `json:"items"`

	NotificationTargets    []CheckinNotificationBackup   `json:"notification_targets,omitempty"`
	NotificationDeliveries []CheckinNotificationDelivery `json:"notification_deliveries,omitempty"`
	Turnstile              *CheckinTurnstileBackup       `json:"turnstile,omitempty"`
}
