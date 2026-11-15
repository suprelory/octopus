package model

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ProxySelection is shared by consumers; inherit is only meaningful for accounts.
type ProxySelection struct {
	Mode     string `json:"proxy_mode"`
	ConfigID *int   `json:"proxy_config_id,omitempty"`
	URL      string `json:"proxy_url,omitempty"`
}

type ProxyConfiguration struct {
	ID             int       `json:"id" gorm:"primaryKey"`
	UID            string    `json:"-" gorm:"size:36;uniqueIndex;not null"`
	Name           string    `json:"name" gorm:"size:128;uniqueIndex;not null"`
	URL            string    `json:"url" gorm:"type:text"` // Endpoint only; authentication is encrypted separately.
	AuthCipher     string    `json:"-" gorm:"type:text"`
	Enabled        bool      `json:"enabled"`
	Remark         string    `json:"remark" gorm:"size:1024"`
	Revision       int64     `json:"revision"`
	RouteRevision  int64     `json:"route_revision"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	HasAuth        bool      `json:"has_auth" gorm:"-"`
	DisplayURL     string    `json:"display_url,omitempty" gorm:"-"`
	ReferenceCount int       `json:"reference_count" gorm:"-"`
}

type ProxyConfigurationBackup struct {
	ProxyConfiguration
	Identity string `json:"identity"`
	Cipher   string `json:"auth_cipher,omitempty"`
}

type ProxyWrite struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	Remark    string `json:"remark"`
	Revision  int64  `json:"revision"`
	ClearAuth bool   `json:"clear_auth"`
}

type ProxyReference struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	SiteID    string `json:"site_id,omitempty"`
	SiteName  string `json:"site_name,omitempty"`
	Inherited bool   `json:"inherited"`
}

// ResolvedProxy is an immutable execution snapshot, never an API response.
type ResolvedProxy struct {
	URL         string
	Fingerprint string
	ConfigID    int
}

func ParseProxyURL(value string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u == nil || u.Opaque != "" || u.Hostname() == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid proxy URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "socks":
		u.Scheme = "socks5"
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("proxy must use http, https, socks5 or socks5h")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid proxy port")
		}
	}
	if strings.HasSuffix(u.Host, ":") || strings.ContainsAny(u.Host, "\\\t\r\n ") {
		return nil, errors.New("invalid proxy host")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path, u.RawPath = "", ""
	return u, nil
}

func NormalizeProxySelection(selection ProxySelection, allowInherit bool) (ProxySelection, error) {
	selection.Mode = strings.TrimSpace(selection.Mode)
	switch selection.Mode {
	case "inherit":
		if !allowInherit {
			return selection, errors.New("proxy inheritance is only supported for checkin accounts")
		}
	case "direct", "system":
	case "pool":
		if selection.ConfigID == nil || *selection.ConfigID <= 0 {
			return selection, errors.New("select a proxy node")
		}
	case "custom":
		u, err := ParseProxyURL(selection.URL)
		if err != nil {
			return selection, err
		}
		selection.URL = u.String()
	default:
		return selection, errors.New("invalid proxy mode")
	}
	if selection.Mode != "pool" {
		selection.ConfigID = nil
	}
	if selection.Mode != "custom" {
		selection.URL = ""
	}
	return selection, nil
}

func (c ChannelConfig) ProxySelection() ProxySelection {
	mode := c.ProxyMode
	if mode == "" {
		mode = "direct"
		if c.Proxy {
			mode = "system"
			if strings.TrimSpace(c.ChannelProxy) != "" {
				mode = "custom"
			}
		}
	}
	return ProxySelection{Mode: mode, ConfigID: c.ProxyConfigID, URL: c.ChannelProxy}
}

func (c CheckinSiteConfig) ProxySelection() ProxySelection {
	mode := c.ProxyMode
	if mode == "" {
		mode = "direct"
		if c.ProxyURL != "" {
			mode = "custom"
		}
	}
	return ProxySelection{Mode: mode, ConfigID: c.ProxyConfigID, URL: c.ProxyURL}
}

func (a CheckinAccount) ProxySelection(site CheckinSiteConfig) ProxySelection {
	if a.ProxyMode == "" || a.ProxyMode == "inherit" {
		return site.ProxySelection()
	}
	return ProxySelection{Mode: a.ProxyMode, ConfigID: a.ProxyConfigID, URL: a.ProxyURL}
}

func NormalizeCheckinAccountProxy(a *CheckinAccount) error {
	if a.ProxyMode == "" {
		a.ProxyMode = "inherit"
	}
	selection, err := NormalizeProxySelection(ProxySelection{Mode: a.ProxyMode, ConfigID: a.ProxyConfigID, URL: a.ProxyURL}, true)
	if err != nil {
		return InvalidCheckin(err.Error())
	}
	if selection.Mode == "custom" {
		if err := ValidateCheckinURL(a.ProxyURL, true); err != nil {
			return err
		}
	} else {
		a.ProxyURL = ""
	}
	a.ProxyMode, a.ProxyConfigID = selection.Mode, selection.ConfigID
	return nil
}
