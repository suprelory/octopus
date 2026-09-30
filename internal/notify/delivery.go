package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DeliveryTimeout = 10 * time.Second

type Message struct {
	Level     string    `json:"level"`
	Title     string    `json:"title"`
	Text      string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Format    Format    `json:"format,omitempty"`
	// Payload preserves an event source's structured webhook contract. All other
	// channels receive the same title and human-readable text.
	Payload any `json:"-"`
	// Variables contains display-only event details available to templates.
	Variables map[string]string `json:"-"`
}

func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       DeliveryTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Deliver performs one bounded delivery. It never includes destination URLs,
// credentials, raw responses or transport errors in returned errors.
func Deliver(ctx context.Context, client *http.Client, target Target, message Message) error {
	ctx, cancel := context.WithTimeout(ctx, DeliveryTimeout)
	defer cancel()
	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now().UTC()
	}
	var templateErr error
	message, templateErr = applyTemplate(target.config.Templates[target.Kind], message)
	if templateErr != nil {
		return templateErr
	}
	if target.Kind == SMTP {
		return deliverSMTP(ctx, target.config, message)
	}
	var endpoint, contentType string
	var body []byte
	var err error
	contentType = "application/json"
	switch target.Kind {
	case Webhook:
		endpoint = target.config.WebhookURL
		payload := message.Payload
		if payload == nil {
			payload = message
		}
		body, err = json.Marshal(payload)
	case Bark:
		endpoint = target.config.BarkURL
		if message.Format == MarkdownFormat {
			message.Text, err = markdownPlainText(message.Text)
			if err != nil {
				return err
			}
		}
		body, err = json.Marshal(map[string]string{"title": message.Title, "body": message.Text, "group": "Octopus"})
	case ServerChan:
		endpoint = "https://sctapi.ftqq.com/" + target.config.ServerChanKey + ".send"
		contentType = "application/x-www-form-urlencoded"
		body = []byte(url.Values{"title": {message.Title}, "desp": {message.Text}}.Encode())
	case Telegram:
		endpoint = "https://api.telegram.org/bot" + target.config.TelegramBotToken + "/sendMessage"
		payload := map[string]string{"chat_id": target.config.TelegramChatID}
		if message.Format == MarkdownFormat {
			payload["text"], err = telegramMarkdown(message.Title, message.Text, 4000)
			if err != nil {
				return err
			}
			payload["parse_mode"] = "HTML"
		} else {
			// Telegram limits text to 4096 characters. Keep a margin for UTF-16.
			payload["text"] = truncateTelegramText(message.Title+"\n"+message.Text, 4000)
		}
		body, err = json.Marshal(payload)
	default:
		return errors.New("unsupported notification channel")
	}
	if err != nil {
		return errors.New("could not encode notification")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid notification request")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "octopus-notification/1")
	if client == nil {
		client = NewHTTPClient()
	}
	// Enforce redirect behavior even when callers provide their own transport.
	boundedClient := *client
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := boundedClient.Do(req)
	if err != nil {
		return errors.New("notification request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notification HTTP status %d", response.StatusCode)
	}
	if target.Kind == Webhook {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil
	}
	var result struct {
		Code *int  `json:"code"`
		OK   *bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&result); err != nil {
		return errors.New("notification channel returned an invalid response")
	}
	accepted := false
	switch target.Kind {
	case Bark:
		accepted = result.Code != nil && *result.Code == 200
	case ServerChan:
		accepted = result.Code != nil && *result.Code == 0
	case Telegram:
		accepted = result.OK != nil && *result.OK
	}
	if !accepted {
		return errors.New("notification channel rejected the message")
	}
	return nil
}

func truncateTelegramText(text string, maxUnits int) string {
	units := 0
	for index, r := range text {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > maxUnits {
			return strings.TrimSpace(text[:index]) + "…"
		}
		units += width
	}
	return text
}
