package model

import "time"

type CheckinNotificationConfig struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Enabled         bool     `json:"enabled"`
	Events          []string `json:"events"`
	IncludeManual   bool     `json:"include_manual"`
	Scope           string   `json:"scope"`
	SiteIDs         []string `json:"site_ids"`
	CooldownSeconds int      `json:"cooldown_seconds"`
	ThresholdMicros *int64   `json:"threshold_micros,omitempty,string"`
	ThresholdUnit   string   `json:"threshold_unit"`
	Language        string   `json:"language"`
	TitleTemplate   string   `json:"title_template"`
	BodyTemplate    string   `json:"body_template"`
	ProxyMode       string   `json:"proxy_mode"`
	ProxyConfigID   *int     `json:"proxy_config_id,omitempty"`
}

func (c CheckinNotificationConfig) ProxySelection() ProxySelection {
	return ProxySelection{Mode: c.ProxyMode, ConfigID: c.ProxyConfigID}
}

type CheckinNotificationTarget struct {
	ID                 string                    `json:"id" gorm:"type:varchar(36);primaryKey"`
	Config             CheckinNotificationConfig `json:"config" gorm:"type:text;serializer:json"`
	ProxyConfigID      *int                      `json:"-" gorm:"index"`
	ProxyConfiguration *ProxyConfiguration       `json:"-" gorm:"foreignKey:ProxyConfigID;constraint:OnDelete:RESTRICT"`
	ConnectionCipher   string                    `json:"-" gorm:"type:text"`
	HasConnection      bool                      `json:"has_connection" gorm:"-"`
	ConnectionVersion  int64                     `json:"connection_version"`
	Revision           int64                     `json:"revision"`
	LeaseOwner         string                    `json:"-" gorm:"size:36"`
	LeaseUntil         *time.Time                `json:"-"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
	DeletedAt          *time.Time                `json:"deleted_at,omitempty" gorm:"index"`
}

// Only display data belongs in an event. Never add credentials, request URLs,
// headers, or upstream response/error text to this durable snapshot.
type CheckinNotificationEvent struct {
	Kind          string     `json:"kind"`
	Identity      string     `json:"identity"`
	SiteID        string     `json:"site_id"`
	AccountID     string     `json:"account_id"`
	SiteName      string     `json:"site_name"`
	AccountName   string     `json:"account_name"`
	CheckinDate   string     `json:"checkin_date"`
	Timezone      string     `json:"timezone"`
	Status        string     `json:"status"`
	Source        string     `json:"source"`
	RewardMicros  *int64     `json:"reward_micros,omitempty,string"`
	RewardUnit    string     `json:"reward_unit"`
	BalanceMicros *int64     `json:"balance_micros,omitempty,string"`
	BalanceUnit   string     `json:"balance_unit"`
	BalanceAt     *time.Time `json:"balance_at,omitempty"`
	OccurredAt    time.Time  `json:"occurred_at"`
}

type CheckinNotificationDelivery struct {
	ID             string                     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TargetID       string                     `json:"target_id" gorm:"type:varchar(36);index;not null"`
	Target         *CheckinNotificationTarget `json:"-" gorm:"foreignKey:TargetID;constraint:OnDelete:RESTRICT"`
	TargetName     string                     `json:"target_name"`
	TargetKind     string                     `json:"target_kind"`
	TargetRevision int64                      `json:"target_revision"`
	EventKey       string                     `json:"-" gorm:"size:64;uniqueIndex"`
	Bucket         string                     `json:"-" gorm:"size:64;index"`
	Event          CheckinNotificationEvent   `json:"event" gorm:"type:text;serializer:json"`
	Title          string                     `json:"title" gorm:"type:text"`
	Message        string                     `json:"message" gorm:"type:text"`
	Status         string                     `json:"status" gorm:"size:16;index"`
	Reason         string                     `json:"reason" gorm:"size:48"`
	Attempts       int                        `json:"attempts"`
	HTTPStatus     int                        `json:"http_status"`
	NextAttemptAt  *time.Time                 `json:"next_attempt_at,omitempty" gorm:"index"`
	LeaseOwner     string                     `json:"-" gorm:"size:36"`
	LeaseUntil     *time.Time                 `json:"-"`
	CreatedAt      time.Time                  `json:"created_at" gorm:"index"`
	UpdatedAt      time.Time                  `json:"updated_at"`
	FinishedAt     *time.Time                 `json:"finished_at,omitempty"`
}

type CheckinNotificationBackup struct {
	CheckinNotificationTarget
	Cipher string `json:"connection_cipher"`
}
