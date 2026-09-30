// Package notify provides shared outbound notification channels. It does not
// depend on a particular event source or the application's settings storage.
package notify

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Kind string

const (
	Webhook    Kind = "webhook"
	Bark       Kind = "bark"
	ServerChan Kind = "serverchan"
	Telegram   Kind = "telegram"
	SMTP       Kind = "smtp"
)

// Config uses the same channel fields as meta-gateway. Empty fields disable a
// channel; SMTP additionally supports STARTTLS, implicit TLS and local relays.
type Config struct {
	Templates        map[Kind]Template `json:"templates,omitempty"`
	WebhookURL       string            `json:"webhook_url,omitempty"`
	BarkURL          string            `json:"bark_url,omitempty"`
	ServerChanKey    string            `json:"serverchan_key,omitempty"`
	TelegramBotToken string            `json:"telegram_bot_token,omitempty"`
	TelegramChatID   string            `json:"telegram_chat_id,omitempty"`
	SMTPHost         string            `json:"smtp_host,omitempty"`
	SMTPPort         int               `json:"smtp_port,omitempty"`
	SMTPUser         string            `json:"smtp_user,omitempty"`
	SMTPPassword     string            `json:"smtp_password,omitempty"`
	SMTPFrom         string            `json:"smtp_from,omitempty"`
	SMTPTo           string            `json:"smtp_to,omitempty"`
	SMTPTLS          string            `json:"smtp_tls,omitempty"`
}

var (
	serverChanKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	telegramTokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
	telegramChatPattern  = regexp.MustCompile(`^(?:-?[0-9]+|@[A-Za-z0-9_]+)$`)
	smtpHostPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
)

func ParseConfig(raw string) (Config, error) {
	var config Config
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return config, nil
	}
	if len(raw) > 65536 || !strings.HasPrefix(raw, "{") {
		return config, errors.New("notification channels must be a JSON object of at most 64 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		// Decoder errors can contain input values, including credentials.
		return Config{}, errors.New("invalid notification channel configuration")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, errors.New("invalid notification channel configuration")
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// ResolveConfig preserves the old check-in webhook until the channel matrix
// is first saved. An explicit {} disables all channels, including that fallback.
func ResolveConfig(raw, legacyWebhook string) (Config, error) {
	if strings.TrimSpace(raw) == "" {
		config := Config{WebhookURL: legacyWebhook}
		err := config.Validate()
		return config, err
	}
	return ParseConfig(raw)
}

func (c *Config) Validate() error {
	for kind, template := range c.Templates {
		switch kind {
		case Webhook, Bark, ServerChan, Telegram, SMTP:
		default:
			return errors.New("unsupported notification template channel")
		}
		if err := template.Validate(); err != nil {
			return err
		}
	}
	fields := []*string{&c.WebhookURL, &c.BarkURL, &c.ServerChanKey, &c.TelegramBotToken,
		&c.TelegramChatID, &c.SMTPHost, &c.SMTPUser, &c.SMTPFrom, &c.SMTPTo, &c.SMTPTLS}
	for _, field := range fields {
		*field = strings.TrimSpace(*field)
		if len(*field) > 4096 || strings.ContainsAny(*field, "\r\n\x00") {
			return errors.New("notification channel fields must be single-line values of at most 4096 bytes")
		}
	}
	if len(c.SMTPPassword) > 4096 || strings.ContainsAny(c.SMTPPassword, "\r\n\x00") {
		return errors.New("invalid SMTP password format")
	}
	if !validHTTPURL(c.WebhookURL) {
		return errors.New("webhook URL must be an http or https URL without credentials or a fragment")
	}
	if !validHTTPURL(c.BarkURL) {
		return errors.New("Bark URL must be an http or https URL without credentials or a fragment")
	}
	if c.ServerChanKey != "" && !serverChanKeyPattern.MatchString(c.ServerChanKey) {
		return errors.New("invalid ServerChan send key format")
	}
	if c.TelegramBotToken != "" || c.TelegramChatID != "" {
		if !telegramTokenPattern.MatchString(c.TelegramBotToken) || !telegramChatPattern.MatchString(c.TelegramChatID) {
			return errors.New("Telegram requires a valid bot token and chat ID")
		}
	}
	if c.hasSMTP() {
		if !smtpHostPattern.MatchString(c.SMTPHost) && net.ParseIP(c.SMTPHost) == nil {
			return errors.New("SMTP host must be a hostname or IP address without a scheme or port")
		}
		if c.SMTPPort < 0 || c.SMTPPort > 65535 {
			return errors.New("SMTP port must be between 1 and 65535, or 0 for the default")
		}
		if c.SMTPTLS != "" && c.SMTPTLS != "starttls" && c.SMTPTLS != "tls" && c.SMTPTLS != "none" {
			return errors.New("SMTP security must be starttls, tls or none")
		}
		if (c.SMTPUser == "") != (c.SMTPPassword == "") {
			return errors.New("SMTP authentication requires both username and password")
		}
		if c.SMTPTLS == "none" && c.SMTPUser != "" {
			return errors.New("SMTP authentication requires TLS")
		}
		if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
			return errors.New("SMTP requires a valid sender email address")
		}
		if recipients, err := mail.ParseAddressList(c.SMTPTo); err != nil || len(recipients) == 0 || len(recipients) > 20 {
			return errors.New("SMTP requires between 1 and 20 recipient email addresses")
		}
	}
	return nil
}

func validHTTPURL(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

func (c Config) hasSMTP() bool {
	return c.SMTPHost != "" || c.SMTPPort != 0 || c.SMTPUser != "" || c.SMTPPassword != "" || c.SMTPFrom != "" || c.SMTPTo != "" || c.SMTPTLS != ""
}

type Target struct {
	Kind   Kind
	config Config
}

// Targets splits a validated configuration so unrelated channel edits do not
// invalidate another channel's cooldown. Fingerprints never expose secrets.
func (c Config) Targets() []Target {
	var targets []Target
	if c.WebhookURL != "" {
		targets = append(targets, Target{Webhook, Config{WebhookURL: c.WebhookURL}})
	}
	if c.BarkURL != "" {
		targets = append(targets, Target{Bark, Config{BarkURL: c.BarkURL}})
	}
	if c.ServerChanKey != "" {
		targets = append(targets, Target{ServerChan, Config{ServerChanKey: c.ServerChanKey}})
	}
	if c.TelegramBotToken != "" && c.TelegramChatID != "" {
		targets = append(targets, Target{Telegram, Config{TelegramBotToken: c.TelegramBotToken, TelegramChatID: c.TelegramChatID}})
	}
	if c.SMTPHost != "" && c.SMTPFrom != "" && c.SMTPTo != "" {
		targets = append(targets, Target{SMTP, Config{
			SMTPHost: c.SMTPHost, SMTPPort: c.SMTPPort, SMTPUser: c.SMTPUser,
			SMTPPassword: c.SMTPPassword, SMTPFrom: c.SMTPFrom, SMTPTo: c.SMTPTo, SMTPTLS: c.SMTPTLS,
		}})
	}
	for i := range targets {
		if template, ok := c.Templates[targets[i].Kind]; ok {
			targets[i].config.Templates = map[Kind]Template{targets[i].Kind: template}
		}
	}
	return targets
}

func (t Target) Fingerprint() [32]byte {
	config := t.config
	config.Templates = nil // Content edits must not bypass delivery cooldowns.
	encoded, _ := json.Marshal(config)
	return sha256.Sum256(append([]byte(t.Kind+":"), encoded...))
}
