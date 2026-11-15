package model

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/turnstile"
)

type CheckinTurnstileSite struct {
	SiteKey       string `json:"site_key"`
	PagePath      string `json:"page_path"`
	Action        string `json:"action"`
	CData         string `json:"cdata"`
	TokenLocation string `json:"token_location"`
	TokenField    string `json:"token_field"`
}

type CheckinTurnstileProvider struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

type CheckinTurnstileConfig struct {
	Enabled          bool                     `json:"enabled"`
	CloudflareSolver CheckinTurnstileProvider `json:"cloudflare_solver"`
	SolverQ          CheckinTurnstileProvider `json:"solverq"`
}

// Singleton settings, kept out of the unencrypted generic Setting API.
type CheckinTurnstileSettings struct {
	ID                  int                    `json:"-" gorm:"primaryKey"`
	Config              CheckinTurnstileConfig `json:"config" gorm:"type:text;serializer:json"`
	Revision            int64                  `json:"revision"`
	CloudflareKeyCipher string                 `json:"-" gorm:"type:text"`
	SolverQKeyCipher    string                 `json:"-" gorm:"type:text"`
	HasCloudflareKey    bool                   `json:"has_cloudflare_key" gorm:"-"`
	HasSolverQKey       bool                   `json:"has_solverq_key" gorm:"-"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

type CheckinTurnstileBackup struct {
	CheckinTurnstileSettings
	CloudflareCipher string `json:"cloudflare_key_cipher"`
	SolverQCipher    string `json:"solverq_key_cipher"`
}

func DefaultCheckinTurnstileSettings() CheckinTurnstileSettings {
	return CheckinTurnstileSettings{ID: 1, Config: CheckinTurnstileConfig{
		CloudflareSolver: CheckinTurnstileProvider{BaseURL: turnstile.CloudflareSolverBaseURL},
		SolverQ:          CheckinTurnstileProvider{BaseURL: turnstile.SolverQBaseURL},
	}}
}

func NormalizeCheckinTurnstile(c *CheckinTurnstileConfig) error {
	var err error
	c.CloudflareSolver.BaseURL, err = turnstile.NormalizeBaseURL(c.CloudflareSolver.BaseURL, turnstile.CloudflareSolverBaseURL)
	if err != nil {
		return InvalidCheckin("invalid Cloudflare Solver URL")
	}
	c.SolverQ.BaseURL, err = turnstile.NormalizeBaseURL(c.SolverQ.BaseURL, turnstile.SolverQBaseURL)
	if err != nil {
		return InvalidCheckin("invalid SolverQ URL")
	}
	if c.Enabled && !c.CloudflareSolver.Enabled && !c.SolverQ.Enabled {
		return InvalidCheckin("enable at least one Turnstile solver")
	}
	return nil
}

func NormalizeCheckinTurnstileSite(c *CheckinTurnstileSite) error {
	if c == nil {
		return nil
	}
	c.SiteKey, c.PagePath = strings.TrimSpace(c.SiteKey), strings.TrimSpace(c.PagePath)
	if c.PagePath == "" {
		c.PagePath = "/"
	}
	if c.TokenLocation == "" {
		c.TokenLocation = "query"
	}
	if c.TokenField == "" {
		c.TokenField = "turnstile"
	}
	if !turnstile.ValidWidgetValue(c.SiteKey, 256) || !turnstile.ValidWidgetValue(c.Action, 32) || !turnstile.ValidWidgetValue(c.CData, 255) {
		return InvalidCheckin("invalid Turnstile widget parameters")
	}
	if err := ValidateCheckinRequest(CheckinRequest{Method: http.MethodGet, Path: c.PagePath}); err != nil {
		return InvalidCheckin("Turnstile page path must stay on this site")
	}
	u, err := url.Parse(c.PagePath)
	if err != nil || len(c.PagePath) > 2048 || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(c.PagePath, "{{") {
		return InvalidCheckin("Turnstile page path must not contain queries, fragments or templates")
	}
	if !turnstile.ValidWidgetValue(c.TokenField, 64) || c.TokenField == "" {
		return InvalidCheckin("invalid Turnstile token field")
	}
	// A token must never overwrite account identity or transport control fields.
	if (containsCheckinSecretField(map[string]any{c.TokenField: ""}) && !strings.EqualFold(c.TokenField, "token")) || strings.EqualFold(c.TokenField, "username") || strings.EqualFold(c.TokenField, "user_id") {
		return InvalidCheckin("Turnstile token field cannot replace credentials")
	}
	switch c.TokenLocation {
	case "query", "json":
	case "header":
		switch strings.ToLower(c.TokenField) {
		case "user-agent", "content-type", "accept", "origin", "referer":
			return InvalidCheckin("Turnstile token cannot replace request metadata")
		}
		if err := ValidateCheckinRequest(CheckinRequest{Method: http.MethodGet, Path: "/", Headers: []CheckinHeader{{Name: c.TokenField}}}); err != nil {
			return InvalidCheckin("invalid Turnstile token header")
		}
	default:
		return InvalidCheckin("Turnstile token location must be query, json or header")
	}
	return nil
}

func TurnstileKeyAAD(platform, baseURL string) string { return "turnstile:" + platform + ":" + baseURL }

func IsTurnstileReason(reason string) bool {
	switch reason {
	case "turnstile_required", "turnstile_disabled", "turnstile_sitekey_missing", "turnstile_solved", "turnstile_rejected", "turnstile_invalid_config", "turnstile_timeout", "turnstile_solver_unavailable", "turnstile_solver_response_invalid", "turnstile_solver_key_invalid", "turnstile_solver_balance_insufficient", "turnstile_solver_busy", "turnstile_unsolvable":
		return true
	}
	return false
}
