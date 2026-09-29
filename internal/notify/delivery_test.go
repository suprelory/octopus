package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPChannelsDeliverExpectedPayloads(t *testing.T) {
	config := Config{
		WebhookURL: "https://webhook.example/hook?key=secret", BarkURL: "https://bark.example/device",
		ServerChanKey: "SCTsecret", TelegramBotToken: "123:secret", TelegramChatID: "-100123",
	}
	message := Message{Level: "info", Title: "签到成功", Text: "站点：示例\n奖励：2.5", Timestamp: time.Now().UTC()}
	for _, target := range config.Targets() {
		t.Run(string(target.Kind), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.Header.Get("User-Agent") != "octopus-notification/1" {
					t.Fatal("incorrect request method or user agent")
				}
				body, _ := io.ReadAll(r.Body)
				response := ""
				if target.Kind == ServerChan {
					form, _ := url.ParseQuery(string(body))
					if r.URL.String() != "https://sctapi.ftqq.com/SCTsecret.send" || form.Get("title") != message.Title || form.Get("desp") != message.Text || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Fatal("incorrect ServerChan request")
					}
					response = `{"code":0}`
				} else {
					var payload map[string]any
					if err := json.Unmarshal(body, &payload); err != nil || r.Header.Get("Content-Type") != "application/json" {
						t.Fatalf("invalid JSON request: %v", err)
					}
					switch target.Kind {
					case Webhook:
						if r.URL.String() != config.WebhookURL || payload["level"] != "info" || payload["title"] != message.Title || payload["message"] != message.Text || payload["timestamp"] == nil {
							t.Fatal("incorrect webhook request")
						}
					case Bark:
						if r.URL.String() != config.BarkURL || payload["title"] != message.Title || payload["body"] != message.Text || payload["group"] != "Octopus" {
							t.Fatal("incorrect Bark request")
						}
						response = `{"code":200}`
					case Telegram:
						if r.URL.String() != "https://api.telegram.org/bot123:secret/sendMessage" || payload["chat_id"] != "-100123" || payload["text"] != message.Title+"\n"+message.Text || payload["parse_mode"] != nil {
							t.Fatal("incorrect Telegram request")
						}
						response = `{"ok":true}`
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
			})}
			if err := Deliver(context.Background(), client, target, message); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPChannelsRejectBusinessErrorsWithoutExposingSecrets(t *testing.T) {
	config := Config{WebhookURL: "https://webhook.example/secret-marker", BarkURL: "https://bark.example/secret-marker", ServerChanKey: "SCTsecret-marker", TelegramBotToken: "123:secret-marker", TelegramChatID: "1"}
	for _, target := range config.Targets() {
		for _, response := range []struct {
			status int
			body   string
			err    error
		}{
			{500, "secret-marker", nil},
			{302, "secret-marker", nil},
			{200, `{"code":500,"ok":false,"message":"secret-marker"}`, nil},
			{200, `<html>secret-marker</html>`, nil},
			{200, `{}`, nil},
			{0, "", errors.New("transport failed: secret-marker")},
		} {
			if target.Kind == Webhook && response.status == 200 {
				continue // Generic webhook success is defined by its HTTP status.
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if response.err != nil {
					return nil, response.err
				}
				return &http.Response{StatusCode: response.status, Body: io.NopCloser(strings.NewReader(response.body)), Header: make(http.Header)}, nil
			})}
			err := Deliver(context.Background(), client, target, Message{Title: "test"})
			if err == nil || strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("%s accepted a failure or exposed a secret: %v", target.Kind, err)
			}
		}
	}
}

func TestTelegramLongUnicodeMessageIsBounded(t *testing.T) {
	text := truncateTelegramText(strings.Repeat("😀", 3000), 4000)
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "…") {
		t.Fatal("truncation corrupted Unicode or omitted the marker")
	}
	units := 0
	for _, r := range text {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units > 4096 {
		t.Fatal("Telegram text exceeds the protocol limit")
	}
}
