package model

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

func NormalizeSiteGroupKey(value string) string {
	key := strings.TrimSpace(value)
	if key == "" {
		return SiteDefaultGroupKey
	}
	return key
}

func NormalizeSiteTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func ValidateSiteTags(tags []string) error {
	if len(tags) > SiteTagsMaxCount {
		return fmt.Errorf("site tags must not exceed %d", SiteTagsMaxCount)
	}
	for _, tag := range tags {
		if utf8.RuneCountInString(tag) > SiteTagMaxLength {
			return fmt.Errorf("site tag %q must not exceed %d characters", tag, SiteTagMaxLength)
		}
	}
	return nil
}

func NormalizeSiteGroupName(groupKey string, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(groupKey); trimmed != "" {
		return trimmed
	}
	return SiteDefaultGroupName
}

func (p SitePlatform) Validate() error {
	switch p {
	case SitePlatformNewAPI, SitePlatformAnyRouter, SitePlatformOneAPI, SitePlatformOneHub, SitePlatformDoneHub,
		SitePlatformSub2API, SitePlatformAPI:
		return nil
	default:
		return fmt.Errorf("unsupported site platform: %s", p)
	}
}

func (t SiteCredentialType) Validate() error {
	switch t {
	case SiteCredentialTypeUsernamePassword, SiteCredentialTypeAccessToken, SiteCredentialTypeAPIKey:
		return nil
	default:
		return fmt.Errorf("unsupported site credential type: %s", t)
	}
}

func (s *Site) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	if s.ExternalCheckinURL != nil {
		trimmed := strings.TrimRight(strings.TrimSpace(*s.ExternalCheckinURL), "/")
		if trimmed == "" {
			s.ExternalCheckinURL = nil
		} else {
			s.ExternalCheckinURL = &trimmed
		}
	}
	s.CheckinTimezone = strings.TrimSpace(s.CheckinTimezone)
	if s.CheckinTimezone == "" {
		s.CheckinTimezone = DefaultSiteCheckinTimezone
	}
	s.CheckinWindowStart = strings.TrimSpace(s.CheckinWindowStart)
	if s.CheckinWindowStart == "" {
		s.CheckinWindowStart = DefaultSiteCheckinWindowStart
	}
	s.CheckinWindowEnd = strings.TrimSpace(s.CheckinWindowEnd)
	if s.CheckinWindowEnd == "" {
		s.CheckinWindowEnd = DefaultSiteCheckinWindowEnd
	}
	if strings.TrimSpace(string(s.ProxyMode)) == "" {
		s.ProxyMode = ProxyUsageModeDirect
	}
	if s.ProxyMode != ProxyUsageModePool {
		s.ProxyConfigID = nil
	}
	if s.GlobalWeight <= 0 {
		s.GlobalWeight = 1
	}
	if s.SortOrder < 0 {
		s.SortOrder = 0
	}
	s.Tags = NormalizeSiteTags(s.Tags)
	s.RouteBaseURLs = NormalizeSiteRouteBaseURLs(s.RouteBaseURLs)
	if s.DefaultRouteType != "" {
		s.DefaultRouteType = NormalizeSiteModelRouteType(s.DefaultRouteType)
	}
	s.normalizeLegacyAPIPlatform()
}

func (s *Site) normalizeLegacyAPIPlatform() {
	switch s.Platform {
	case "openai":
		s.Platform = SitePlatformAPI
		if s.DefaultRouteType == "" {
			s.DefaultRouteType = SiteModelRouteTypeOpenAIChat
		}
	case "claude":
		s.Platform = SitePlatformAPI
		if s.DefaultRouteType == "" {
			s.DefaultRouteType = SiteModelRouteTypeAnthropic
		}
	case "gemini":
		s.Platform = SitePlatformAPI
		if s.DefaultRouteType == "" {
			s.DefaultRouteType = SiteModelRouteTypeGemini
		}
	}
}

func (s *Site) Validate() error {
	if s == nil {
		return fmt.Errorf("site is nil")
	}
	s.Normalize()
	if s.Name == "" {
		return fmt.Errorf("site name is required")
	}
	if err := s.Platform.Validate(); err != nil {
		return err
	}
	if s.DefaultRouteType == SiteModelRouteTypeUnknown {
		return fmt.Errorf("site default route type is unsupported")
	}
	if err := s.ProxyMode.Validate(false); err != nil {
		return err
	}
	if s.ProxyMode == ProxyUsageModePool && (s.ProxyConfigID == nil || *s.ProxyConfigID <= 0) {
		return fmt.Errorf("proxy config id is required when proxy mode is pool")
	}
	if err := ValidateSiteTags(s.Tags); err != nil {
		return err
	}
	parsed, err := url.Parse(s.BaseURL)
	if err != nil {
		return fmt.Errorf("site base url is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("site base url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("site base url must have a host")
	}
	if err := ValidateSiteRouteBaseURLs(s.RouteBaseURLs); err != nil {
		return err
	}
	if s.ExternalCheckinURL != nil {
		checkinParsed, err := url.Parse(*s.ExternalCheckinURL)
		if err != nil {
			return fmt.Errorf("external checkin url is invalid: %w", err)
		}
		if checkinParsed.Scheme != "http" && checkinParsed.Scheme != "https" {
			return fmt.Errorf("external checkin url must use http or https")
		}
		if checkinParsed.Host == "" {
			return fmt.Errorf("external checkin url must have a host")
		}
	}
	if _, err := time.LoadLocation(s.CheckinTimezone); err != nil {
		return fmt.Errorf("site checkin timezone is invalid: %w", err)
	}
	windowStart, err := time.Parse("15:04", s.CheckinWindowStart)
	if err != nil {
		return fmt.Errorf("site checkin window start must use HH:MM format")
	}
	windowEnd, err := time.Parse("15:04", s.CheckinWindowEnd)
	if err != nil {
		return fmt.Errorf("site checkin window end must use HH:MM format")
	}
	if windowEnd.Before(windowStart) {
		return fmt.Errorf("site checkin window end must not be before start")
	}
	return nil
}

func (a *SiteAccount) Normalize() {
	a.Name = strings.TrimSpace(a.Name)
	a.Username = strings.TrimSpace(a.Username)
	a.Password = strings.TrimSpace(a.Password)
	a.AccessToken = strings.TrimSpace(a.AccessToken)
	a.APIKey = strings.TrimSpace(a.APIKey)
	a.RefreshToken = strings.TrimSpace(a.RefreshToken)
	if a.TokenExpiresAt < 0 {
		a.TokenExpiresAt = 0
	}
	if a.TokenExpiresAt > 0 && a.TokenExpiresAt < 1_000_000_000_000 {
		a.TokenExpiresAt *= 1000
	}
	if a.PlatformUserID != nil && *a.PlatformUserID <= 0 {
		a.PlatformUserID = nil
	}
	if strings.TrimSpace(string(a.ProxyMode)) == "" {
		a.ProxyMode = ProxyUsageModeInherit
	}
	if a.ProxyMode != ProxyUsageModePool {
		a.ProxyConfigID = nil
	}
	if a.CheckinIntervalHours <= 0 {
		a.CheckinIntervalHours = 24
	}
	if a.CheckinRandomWindowMinutes < 0 {
		a.CheckinRandomWindowMinutes = 0
	}
}

func (a *SiteAccount) Validate() error {
	if a == nil {
		return fmt.Errorf("site account is nil")
	}
	a.Normalize()
	if a.SiteID == 0 {
		return fmt.Errorf("site id is required")
	}
	if a.Name == "" {
		return fmt.Errorf("site account name is required")
	}
	if err := a.CredentialType.Validate(); err != nil {
		return err
	}
	if err := a.ProxyMode.Validate(true); err != nil {
		return err
	}
	if a.ProxyMode == ProxyUsageModePool && (a.ProxyConfigID == nil || *a.ProxyConfigID <= 0) {
		return fmt.Errorf("proxy config id is required when proxy mode is pool")
	}
	if a.CheckinIntervalHours <= 0 {
		return fmt.Errorf("checkin interval hours must be greater than 0")
	}
	if a.CheckinIntervalHours > 720 {
		return fmt.Errorf("checkin interval hours must be less than or equal to 720")
	}
	if a.CheckinRandomWindowMinutes < 0 {
		return fmt.Errorf("checkin random window minutes must be greater than or equal to 0")
	}
	if a.CheckinRandomWindowMinutes > 1440 {
		return fmt.Errorf("checkin random window minutes must be less than or equal to 1440")
	}
	if a.PlatformUserID != nil && *a.PlatformUserID <= 0 {
		return fmt.Errorf("platform user id must be greater than 0")
	}
	if a.TokenExpiresAt < 0 {
		return fmt.Errorf("token expires at must be greater than or equal to 0")
	}
	switch a.CredentialType {
	case SiteCredentialTypeUsernamePassword:
		if a.Username == "" || a.Password == "" {
			return fmt.Errorf("username and password are required")
		}
	case SiteCredentialTypeAccessToken:
		if a.AccessToken == "" {
			return fmt.Errorf("access token is required")
		}
	case SiteCredentialTypeAPIKey:
		if a.APIKey == "" {
			return fmt.Errorf("api key is required")
		}
	}
	return nil
}
