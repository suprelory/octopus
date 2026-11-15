package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

type Result struct {
	Status     string
	Reason     string
	HTTPStatus int
	RetryAfter time.Duration
}

// Deliver never returns raw transport errors or remote messages: they can
// contain webhook URLs, bot tokens, credentials, or reflected request bodies.
func Deliver(ctx context.Context, client *http.Client, kind string, connection Connection, title, message string, values map[string]string, now time.Time) Result {
	if err := NormalizeConnection(kind, &connection); err != nil {
		return Result{Status: "failed", Reason: "invalid_connection"}
	}
	if kind == "smtp" {
		return deliverSMTP(ctx, *connection.SMTP, title, message)
	}
	endpoint, payload, contentType := connection.URL, []byte(nil), "application/json"
	combined := title + "\n\n" + message
	var body any
	switch kind {
	case "webhook":
		parameters := map[string]string{}
		for k, v := range values {
			parameters[k] = v
		}
		parameters["title"], parameters["message"] = title, message
		var err error
		payload, err = webhookPayload(connection.Payload, parameters)
		if err != nil {
			return Result{Status: "failed", Reason: "invalid_template"}
		}
	case "bark":
		body = map[string]string{"title": truncateBytes(title, 1024), "body": message, "group": "Octopus"}
	case "serverchan":
		endpoint = "https://sctapi.ftqq.com/" + connection.SendKey + ".send"
		payload = []byte(url.Values{"title": {truncateBytes(title, 256)}, "desp": {message}}.Encode())
		contentType = "application/x-www-form-urlencoded"
	case "telegram":
		endpoint = connection.BaseURL + "/bot" + connection.Token + "/sendMessage"
		body = map[string]any{"chat_id": connection.ChatID, "text": truncateUTF16(combined, 4000), "disable_web_page_preview": true}
	case "feishu":
		object := map[string]any{"msg_type": "text", "content": map[string]string{"text": truncateBytes(combined, 18000)}}
		if connection.Secret != "" {
			timestamp := strconv.FormatInt(now.Unix(), 10)
			mac := hmac.New(sha256.New, []byte(timestamp+"\n"+connection.Secret))
			object["timestamp"], object["sign"] = timestamp, base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		body = object
	case "dingtalk", "wecom":
		limit := 18000
		if kind == "wecom" {
			limit = 2048
		}
		body = map[string]any{"msgtype": "text", "text": map[string]string{"content": truncateBytes(combined, limit)}}
		if kind == "dingtalk" && connection.Secret != "" {
			timestamp := strconv.FormatInt(now.UnixMilli(), 10)
			mac := hmac.New(sha256.New, []byte(connection.Secret))
			_, _ = mac.Write([]byte(timestamp + "\n" + connection.Secret))
			u, _ := url.Parse(endpoint)
			query := u.Query()
			query.Set("timestamp", timestamp)
			query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
			u.RawQuery, endpoint = query.Encode(), ""
			endpoint = u.String()
		}
	case "ntfy":
		endpoint = connection.BaseURL
		body = map[string]any{"topic": connection.Topic, "title": truncateBytes(title, 256), "message": truncateBytes(message, 4096), "priority": connection.Priority}
	}
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Result{Status: "failed", Reason: "invalid_connection"}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Octopus-Notifications/1.0")
	if kind == "webhook" {
		for _, h := range connection.Headers {
			req.Header.Set(h.Name, h.Value)
		}
	}
	if kind == "ntfy" && connection.Token != "" {
		req.Header.Set("Authorization", "Bearer "+connection.Token)
	}
	// Do not follow redirects, even with a caller-supplied client: credentials
	// frequently live in the URL, and a redirected POST may become a GET.
	configured := *client
	configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if configured.Timeout <= 0 || configured.Timeout > 20*time.Second {
		configured.Timeout = 20 * time.Second
	}
	response, err := configured.Do(req)
	if err != nil {
		return Result{Status: "uncertain", Reason: "network_uncertain"}
	}
	defer response.Body.Close()
	code := response.StatusCode
	if code == http.StatusTooManyRequests {
		return Result{Status: "queued", Reason: "rate_limited", HTTPStatus: code, RetryAfter: retryAfter(response.Header.Get("Retry-After"), now)}
	}
	if code >= 300 && code < 400 {
		return Result{Status: "failed", Reason: "redirect_blocked", HTTPStatus: code}
	}
	if code >= 500 || code == http.StatusRequestTimeout {
		return Result{Status: "uncertain", Reason: "remote_uncertain", HTTPStatus: code}
	}
	if code < 200 || code >= 300 {
		return Result{Status: "failed", Reason: "remote_rejected", HTTPStatus: code}
	}
	if kind == "webhook" {
		return Result{Status: "sent", Reason: "accepted", HTTPStatus: code}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return Result{Status: "uncertain", Reason: "invalid_response", HTTPStatus: code}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return Result{Status: "uncertain", Reason: "invalid_response", HTTPStatus: code}
	}
	accepted, explicit := false, false
	switch kind {
	case "telegram":
		var ok *bool
		if json.Unmarshal(object["ok"], &ok) == nil && ok != nil {
			explicit, accepted = true, *ok
		}
	case "ntfy":
		var id, event, topic string
		if json.Unmarshal(object["id"], &id) == nil && json.Unmarshal(object["event"], &event) == nil && json.Unmarshal(object["topic"], &topic) == nil {
			accepted = id != "" && event == "message" && topic == connection.Topic
			explicit = accepted
		}
	default:
		key, success := "code", 0
		if kind == "bark" {
			success = 200
		}
		if kind == "dingtalk" || kind == "wecom" {
			key = "errcode"
		}
		var value *int
		if json.Unmarshal(object[key], &value) == nil && value != nil {
			explicit, accepted = true, *value == success
		}
	}
	if accepted {
		return Result{Status: "sent", Reason: "accepted", HTTPStatus: code}
	}
	if explicit {
		return Result{Status: "failed", Reason: "remote_rejected", HTTPStatus: code}
	}
	return Result{Status: "uncertain", Reason: "invalid_response", HTTPStatus: code}
}

func retryAfter(value string, now time.Time) time.Duration {
	delay := 30 * time.Second
	if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 86400 {
		delay = time.Duration(n) * time.Second
	} else if date, err := http.ParseTime(value); err == nil {
		delay = date.Sub(now)
	}
	if delay < time.Second {
		delay = time.Second
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return delay
}

func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := 0
	for i := range value {
		if i > limit-3 {
			break
		}
		end = i
	}
	return value[:end] + "…"
}

func truncateUTF16(value string, limit int) string {
	units := 0
	for _, r := range value {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units <= limit {
		return value
	}
	units = 0
	var result strings.Builder
	for _, r := range value {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > limit-1 {
			break
		}
		result.WriteRune(r)
		units += n
	}
	return result.String() + "…"
}

func DeliveryValues(row model.CheckinNotificationDelivery, language string) map[string]string {
	values := Values(row.Event, language)
	// Machine-facing Webhook payloads use stable event codes, not translations.
	values["event"] = row.Event.Kind
	return values
}
