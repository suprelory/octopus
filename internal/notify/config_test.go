package notify

import (
	"strings"
	"testing"
)

func TestConfigValidationAndLegacyFallback(t *testing.T) {
	for _, raw := range []string{
		"", `{}`, `{"webhook_url":" https://example.com/notify?key=secret "}`,
		`{"bark_url":"https://api.day.app/device-key","serverchan_key":"SCTtest"}`,
		`{"telegram_bot_token":"123:test-token","telegram_chat_id":"-100123"}`,
		`{"smtp_host":"mail.example","smtp_from":"Octopus <a@example.com>","smtp_to":"b@example.com, c@example.com","smtp_port":465}`,
		`{"smtp_host":"127.0.0.1","smtp_from":"a@example.com","smtp_to":"b@example.com","smtp_tls":"none"}`,
	} {
		if _, err := ParseConfig(raw); err != nil {
			t.Fatalf("valid configuration rejected: %v", err)
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{"webhook_url":true}`, `{} {}`, `{"unknown_secret":"secret-marker"}`,
		`{"webhook_url":"file:///secret-marker"}`, `{"bark_url":"https://user:secret-marker@example.com"}`,
		`{"webhook_url":"https://example.com/#secret-marker"}`, `{"webhook_url":"http://example.com:65536"}`,
		`{"serverchan_key":"secret-marker/path"}`, `{"telegram_bot_token":"123:secret-marker"}`,
		`{"telegram_bot_token":"123:secret-marker/sendMessage","telegram_chat_id":"1"}`,
		`{"telegram_bot_token":"123:secret-marker","telegram_chat_id":"1\n2"}`,
		`{"smtp_host":"smtp://example.com","smtp_password":"secret-marker"}`,
		`{"smtp_host":"example.com","smtp_from":"a@example.com","smtp_to":"bad","smtp_password":"secret-marker"}`,
		`{"smtp_host":"example.com","smtp_from":"a@example.com\r\nBcc: b@example.com","smtp_to":"b@example.com"}`,
		`{"smtp_host":"example.com","smtp_from":"a@example.com","smtp_to":"b@example.com","smtp_port":-1}`,
		`{"smtp_host":"example.com","smtp_from":"a@example.com","smtp_to":"b@example.com","smtp_tls":"none","smtp_user":"user","smtp_password":"secret-marker"}`,
		`{"smtp_host":"example.com","smtp_from":"a@example.com","smtp_to":"b@example.com","smtp_tls":"invalid"}`,
	} {
		_, err := ParseConfig(raw)
		if err == nil || strings.Contains(err.Error(), "secret-marker") {
			t.Fatalf("invalid configuration was accepted or exposed a secret: %v", err)
		}
	}
	legacy, err := ResolveConfig("", " https://legacy.example/hook ")
	if err != nil || legacy.WebhookURL != "https://legacy.example/hook" {
		t.Fatalf("legacy webhook did not resolve: %+v, %v", legacy, err)
	}
	empty, err := ResolveConfig(`{}`, legacy.WebhookURL)
	if err != nil || len(empty.Targets()) != 0 {
		t.Fatal("clearing all channels restored the legacy webhook")
	}
	if _, err := ResolveConfig(`{"invalid":true}`, legacy.WebhookURL); err == nil {
		t.Fatal("invalid configuration silently fell back to the legacy webhook")
	}
}

func TestChannelFingerprintOnlyChangesWithItsOwnConfiguration(t *testing.T) {
	config := Config{WebhookURL: "https://webhook.example", BarkURL: "https://bark.example/first"}
	before := config.Targets()
	config.BarkURL = "https://bark.example/second"
	after := config.Targets()
	if before[0].Fingerprint() != after[0].Fingerprint() || before[1].Fingerprint() == after[1].Fingerprint() {
		t.Fatal("channel identity did not isolate destination changes")
	}
}
