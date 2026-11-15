// Package notify renders bounded messages and delivers them without exposing
// connection secrets or remote error text to its callers.
package notify

import (
	"encoding/json"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
)

var Kinds = []string{"webhook", "telegram", "bark", "serverchan", "smtp", "feishu", "dingtalk", "wecom", "ntfy"}
var Events = []string{"success", "already_checked_in", "failed", "attention", "low_balance"}

type SMTPConfig struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	TLS      string   `json:"tls"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

// The entire connection, including URLs and custom headers, is encrypted.
// API responses only indicate whether a saved connection exists.
type Connection struct {
	URL      string                `json:"url,omitempty"`
	Token    string                `json:"token,omitempty"`
	ChatID   string                `json:"chat_id,omitempty"`
	SendKey  string                `json:"send_key,omitempty"`
	Secret   string                `json:"secret,omitempty"`
	BaseURL  string                `json:"base_url,omitempty"`
	Topic    string                `json:"topic,omitempty"`
	Priority int                   `json:"priority,omitempty"`
	Headers  []model.CheckinHeader `json:"headers,omitempty"`
	Payload  string                `json:"payload,omitempty"`
	SMTP     *SMTPConfig           `json:"smtp,omitempty"`
}

func invalid(message string) error { return model.InvalidCheckin(message) }

func NormalizeConfig(c *model.CheckinNotificationConfig) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 128 || strings.ContainsAny(c.Name, "\r\n\x00") {
		return invalid("notification name is required (maximum 128 bytes)")
	}
	if !slices.Contains(Kinds, c.Kind) {
		return invalid("unsupported notification platform")
	}
	if len(c.Events) == 0 || len(c.Events) > len(Events) {
		return invalid("select at least one notification event")
	}
	seen := map[string]bool{}
	for _, event := range c.Events {
		if !slices.Contains(Events, event) || seen[event] {
			return invalid("invalid or duplicate notification event")
		}
		seen[event] = true
	}
	switch c.Scope {
	case "all":
		if len(c.SiteIDs) != 0 {
			return invalid("all-sites scope must not contain site IDs")
		}
	case "sites":
		if len(c.SiteIDs) == 0 || len(c.SiteIDs) > 1000 {
			return invalid("select notification sites")
		}
		ids := map[string]bool{}
		for _, id := range c.SiteIDs {
			if _, err := uuid.Parse(id); err != nil || ids[id] {
				return invalid("invalid or duplicate notification site ID")
			}
			ids[id] = true
		}
	default:
		return invalid("notification scope must be all or sites")
	}
	if c.CooldownSeconds < 0 || c.CooldownSeconds > 604800 {
		return invalid("notification cooldown must be between 0 and 604800 seconds")
	}
	if seen["low_balance"] {
		if c.ThresholdMicros == nil || *c.ThresholdMicros < 0 || (c.ThresholdUnit != "USD" && c.ThresholdUnit != "quota") {
			return invalid("low-balance notifications require a nonnegative threshold and USD or quota unit")
		}
	} else {
		c.ThresholdMicros, c.ThresholdUnit = nil, ""
	}
	if c.Language == "" {
		c.Language = "en"
	}
	if !slices.Contains([]string{"en", "zh_hans", "zh_hant"}, c.Language) {
		return invalid("invalid notification language")
	}
	if err := validateTemplate(c.TitleTemplate, 512, false); err != nil {
		return err
	}
	if err := validateTemplate(c.BodyTemplate, 8192, false); err != nil {
		return err
	}
	if c.ProxyMode == "" {
		c.ProxyMode = "direct"
	}
	if c.ProxyMode == "custom" || c.ProxyMode == "inherit" {
		return invalid("notifications support direct, system or shared-pool proxies")
	}
	p, err := model.NormalizeProxySelection(c.ProxySelection(), false)
	if err != nil {
		return invalid("invalid notification proxy selection")
	}
	if c.Kind == "smtp" && p.Mode != "direct" {
		return invalid("SMTP notifications require a direct connection")
	}
	c.ProxyMode, c.ProxyConfigID = p.Mode, p.ConfigID
	return nil
}

func endpoint(value string) error {
	if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
		return invalid("invalid notification URL")
	}
	u, err := url.Parse(value)
	if err != nil || u == nil || u.Hostname() == "" || u.Opaque != "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return invalid("notification URL must be an HTTP(S) endpoint without user information or fragment")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return invalid("invalid notification URL port")
		}
	}
	return nil
}

var tokenRE = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var chatRE = regexp.MustCompile(`^(-?[0-9]+|@[A-Za-z0-9_]{1,64})$`)
var topicRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var keyRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
var headerRE = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

func NormalizeConnection(kind string, c *Connection) error {
	encoded, err := json.Marshal(c)
	if err != nil || len(encoded) > 65536 {
		return invalid("notification connection exceeds size limits")
	}
	for _, v := range []string{c.Token, c.ChatID, c.SendKey, c.Secret, c.Topic} {
		if len(v) > 8192 || strings.ContainsAny(v, "\r\n\x00") {
			return invalid("invalid notification connection field")
		}
	}
	c.URL, c.BaseURL = strings.TrimSpace(c.URL), strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	switch kind {
	case "webhook", "bark", "feishu", "dingtalk", "wecom":
		if err := endpoint(c.URL); err != nil {
			return err
		}
	case "telegram":
		if !tokenRE.MatchString(c.Token) || !chatRE.MatchString(c.ChatID) {
			return invalid("invalid Telegram bot token or chat ID")
		}
		if c.BaseURL == "" {
			c.BaseURL = "https://api.telegram.org"
		}
	case "serverchan":
		if !keyRE.MatchString(c.SendKey) {
			return invalid("invalid ServerChan Turbo SendKey")
		}
	case "ntfy":
		if c.BaseURL == "" {
			c.BaseURL = "https://ntfy.sh"
		}
		if !topicRE.MatchString(c.Topic) {
			return invalid("invalid ntfy topic")
		}
		if c.Priority == 0 {
			c.Priority = 3
		}
		if c.Priority < 1 || c.Priority > 5 {
			return invalid("ntfy priority must be between 1 and 5")
		}
	case "smtp":
		if c.SMTP == nil {
			return invalid("SMTP connection is required")
		}
		if err := normalizeSMTP(c.SMTP); err != nil {
			return err
		}
	default:
		return invalid("unsupported notification platform")
	}
	if kind == "telegram" || kind == "ntfy" {
		if err := endpoint(c.BaseURL); err != nil {
			return err
		}
		u, _ := url.Parse(c.BaseURL)
		if u.RawQuery != "" || u.ForceQuery {
			return invalid("notification API base URL must not contain a query")
		}
	}
	if kind != "webhook" && (len(c.Headers) != 0 || c.Payload != "") {
		return invalid("custom headers and JSON payload are only supported by Webhook")
	}
	if len(c.Headers) > 32 {
		return invalid("too many Webhook headers")
	}
	seen := map[string]bool{}
	for _, h := range c.Headers {
		name := strings.ToLower(h.Name)
		if len(h.Name) > 128 || !headerRE.MatchString(h.Name) || len(h.Value) > 8192 || strings.ContainsAny(h.Value, "\r\n\x00") || seen[name] {
			return invalid("invalid or duplicate Webhook header")
		}
		if slices.Contains([]string{"host", "content-length", "content-type", "connection", "transfer-encoding", "upgrade", "trailer", "te"}, name) || strings.HasPrefix(name, "proxy-") {
			return invalid("reserved Webhook header")
		}
		seen[name] = true
	}
	if c.Payload != "" {
		if len(c.Payload) > 16384 {
			return invalid("Webhook JSON payload is too large")
		}
		var object map[string]any
		if json.Unmarshal([]byte(c.Payload), &object) != nil || object == nil {
			return invalid("Webhook payload must be a JSON object")
		}
		if err := validateJSONTemplate(object, 0); err != nil {
			return err
		}
	}
	return nil
}

func normalizeSMTP(c *SMTPConfig) error {
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" || len(c.Host) > 253 || strings.ContainsAny(c.Host, "\r\n\x00 /\\@") {
		return invalid("invalid SMTP host")
	}
	if c.TLS == "" {
		c.TLS = "starttls"
	}
	if !slices.Contains([]string{"starttls", "tls", "none"}, c.TLS) {
		return invalid("invalid SMTP TLS mode")
	}
	if c.Port == 0 {
		c.Port = 587
		if c.TLS == "tls" {
			c.Port = 465
		}
		if c.TLS == "none" {
			c.Port = 25
		}
	}
	if c.Port < 1 || c.Port > 65535 {
		return invalid("invalid SMTP port")
	}
	if len(c.Username) > 512 || len(c.Password) > 8192 || strings.ContainsAny(c.Username+c.Password, "\r\n\x00") {
		return invalid("invalid SMTP authentication")
	}
	if (c.Username == "") != (c.Password == "") {
		return invalid("SMTP authentication requires both username and password")
	}
	if c.Username != "" && c.TLS == "none" {
		return invalid("SMTP authentication requires TLS")
	}
	if len(c.To) == 0 || len(c.To) > 20 {
		return invalid("SMTP requires 1 to 20 recipients")
	}
	for _, address := range append([]string{c.From}, c.To...) {
		if len(address) > 320 || strings.ContainsAny(address, "\r\n\x00") {
			return invalid("invalid SMTP address")
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || !strings.Contains(parsed.Address, "@") || strings.ContainsAny(parsed.Address, "\r\n\x00") {
			return invalid("invalid SMTP address")
		}
	}
	return nil
}
