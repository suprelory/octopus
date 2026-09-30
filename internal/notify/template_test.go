package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTemplateSubstitutionAndWebhookCompatibility(t *testing.T) {
	payload := map[string]any{"event": "site_checkin_failed", "title": "original", "message": "detail", "account_id": 7}
	message := Message{Title: "original", Text: "full message", Level: "error", Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Payload: payload,
		Variables: map[string]string{"site": "quoted \"site\"\n{{message}}", "detail": "detail", "account": "account"}}
	unchanged, err := applyTemplate(Template{}, message)
	if err != nil || !reflect.DeepEqual(unchanged, message) {
		t.Fatal("default changed the legacy notification")
	}
	rendered, err := applyTemplate(Template{Title: "{{emoji}} {{ title }}", Body: "{{site}} / {{account}} / {{reward}} / {{time}}"}, message)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Title != "❌ original" || rendered.Text != "quoted \"site\"\n{{message}} / account /  / 2026-01-01T12:00:00Z" {
		t.Fatalf("incorrect or recursive rendering: %+v", rendered)
	}
	data, err := json.Marshal(rendered.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["message"] != rendered.Text || got["event"] != payload["event"] || got["account_id"] != float64(7) || payload["message"] != "detail" {
		t.Fatalf("webhook metadata was changed or payload mutated: %s", data)
	}
	titleOnly, err := applyTemplate(Template{Title: "custom"}, message)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(titleOnly.Payload)
	_ = json.Unmarshal(data, &got)
	if got["message"] != "detail" {
		t.Fatal("blank body template changed the legacy webhook body")
	}
	fallback, err := applyTemplate(Template{Title: "{{reward}}", Body: "{{reward}}"}, message)
	if err != nil || fallback.Title != message.Title || fallback.Text != message.Text {
		t.Fatal("missing values did not fall back to original content")
	}
}

func TestTemplateValidationAndFingerprint(t *testing.T) {
	for _, raw := range []string{
		`{"templates":{"unknown":{"body":"test"}}}`,
		`{"templates":{"bark":{"body":"{{password}}"}}}`,
		`{"templates":{"bark":{"body":"{{message"}}}`,
		`{"templates":{"bark":{"title":"line\nbreak"}}}`,
		`{"templates":{"bark":{"extra":"secret-marker"}}}`,
		`{"templates":{"bark":{"body":true}}}`,
	} {
		if _, err := ParseConfig(raw); err == nil || strings.Contains(err.Error(), "secret-marker") {
			t.Fatalf("invalid template accepted or leaked: %v", err)
		}
	}
	if err := (Template{Body: strings.Repeat("中", 3000)}).Validate(); err == nil {
		t.Fatal("oversize UTF-8 template accepted")
	}
	config := Config{WebhookURL: "https://example.invalid/hook", BarkURL: "https://example.invalid/device"}
	before := config.Targets()
	config.Templates = map[Kind]Template{Webhook: {Title: "one"}, Bark: {Title: "two"}}
	encoded, _ := json.Marshal(config)
	restored, err := ParseConfig(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	for i, target := range restored.Targets() {
		if before[i].Fingerprint() != target.Fingerprint() {
			t.Fatal("template edit reset cooldown identity")
		}
		if len(target.config.Templates) != 1 || target.config.Templates[target.Kind] != config.Templates[target.Kind] {
			t.Fatal("template crossed notification channels")
		}
	}
}

func TestHTTPChannelsDeliverTheirOwnTemplate(t *testing.T) {
	config := Config{WebhookURL: "https://example.invalid/hook", BarkURL: "https://example.invalid/device", ServerChanKey: "SCTtest", TelegramBotToken: "123:test", TelegramChatID: "1", Templates: map[Kind]Template{}}
	for _, kind := range []Kind{Webhook, Bark, ServerChan, Telegram} {
		config.Templates[kind] = Template{Title: string(kind) + ": {{title}}", Body: "{{site}}\n{{detail}}"}
	}
	for _, target := range config.Targets() {
		t.Run(string(target.Kind), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(r.Body)
				wantTitle, wantBody := string(target.Kind)+": result", "site\ndetails"
				if target.Kind == ServerChan {
					form, _ := url.ParseQuery(string(body))
					if form.Get("title") != wantTitle || form.Get("desp") != wantBody {
						t.Fatalf("incorrect form: %s", body)
					}
				} else {
					var payload map[string]string
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Fatal(err)
					}
					switch target.Kind {
					case Webhook:
						if payload["title"] != wantTitle || payload["message"] != wantBody {
							t.Fatalf("incorrect webhook: %s", body)
						}
					case Bark:
						if payload["title"] != wantTitle || payload["body"] != wantBody {
							t.Fatalf("incorrect Bark: %s", body)
						}
					case Telegram:
						if payload["text"] != wantTitle+"\n"+wantBody {
							t.Fatalf("incorrect Telegram: %s", body)
						}
					}
				}
				response := `{"code":200,"ok":true}`
				if target.Kind == ServerChan {
					response = `{"code":0}`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			if err := Deliver(context.Background(), client, target, Message{Title: "result", Text: "original", Variables: map[string]string{"site": "site", "detail": "details"}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
