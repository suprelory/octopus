package notify

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestMarkdownTemplateEscapesVariablesAndPreservesWebhookMetadata(t *testing.T) {
	value := `A_* <b>unsafe</b> & [link](https://evil.example) {{title}}`
	payload := map[string]any{"event": "site_checkin_failed", "message": "detail *text*", "account_id": 7}
	message := Message{Title: "result", Text: "original **text**", Payload: payload, Variables: map[string]string{"site": value}}
	rendered, err := applyTemplate(Template{Format: MarkdownFormat, Title: "{{site}}", Body: "**{{site}}**\n{{message}}"}, message)
	if err != nil {
		t.Fatal(err)
	}
	htmlBody, _, err := markdownDocument(rendered.Text)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := markdownPlainText(rendered.Text)
	if err != nil || plain != value+"\noriginal **text**" || rendered.Title != value {
		t.Fatalf("variable values changed or recursed: %q, %v", plain, err)
	}
	if strings.Contains(htmlBody, "<b>") || strings.Contains(htmlBody, "<a ") || !strings.Contains(htmlBody, "<strong>") {
		t.Fatalf("variable supplied formatting: %s", htmlBody)
	}
	encoded, _ := json.Marshal(rendered.Payload)
	var got map[string]any
	_ = json.Unmarshal(encoded, &got)
	if got["format"] != "markdown" || got["event"] != payload["event"] || got["account_id"] != float64(7) || payload["message"] != "detail *text*" {
		t.Fatalf("webhook metadata changed or source was mutated: %s", encoded)
	}
	// Selecting a format alone must not turn legacy event text into Markdown syntax.
	fallback, err := applyTemplate(Template{Format: MarkdownFormat}, message)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ = markdownPlainText(fallback.Text)
	if plain != message.Text || fallback.Format != MarkdownFormat {
		t.Fatalf("format-only template changed the fallback text: %q", plain)
	}
	encoded, _ = json.Marshal(fallback.Payload)
	_ = json.Unmarshal(encoded, &got)
	if got["message"] != escapeMarkdown("detail *text*") {
		t.Fatal("format-only template replaced the legacy webhook detail")
	}
}

func TestMarkdownHTTPChannelConversion(t *testing.T) {
	config := Config{WebhookURL: "https://example.invalid/hook", BarkURL: "https://example.invalid/device", ServerChanKey: "SCTtest", TelegramBotToken: "123:test", TelegramChatID: "1", Templates: map[Kind]Template{}}
	for _, kind := range []Kind{Webhook, Bark, ServerChan, Telegram} {
		config.Templates[kind] = Template{Format: MarkdownFormat, Body: "**{{site}}**\n[details](https://example.com)"}
	}
	for _, target := range config.Targets() {
		t.Run(string(target.Kind), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(request.Body)
				response := `{"code":200,"ok":true}`
				if target.Kind == ServerChan {
					form, _ := url.ParseQuery(string(body))
					if form.Get("desp") != "**site**\n[details](https://example.com)" {
						t.Fatalf("Markdown was not passed through: %s", body)
					}
					response = `{"code":0}`
				} else {
					var got map[string]any
					if err := json.Unmarshal(body, &got); err != nil {
						t.Fatal(err)
					}
					switch target.Kind {
					case Webhook:
						if got["format"] != "markdown" || got["event"] != "site_checkin_failed" || got["message"] != "**site**\n[details](https://example.com)" {
							t.Fatalf("incorrect Markdown webhook: %s", body)
						}
					case Bark:
						if got["body"] != "site\ndetails (https://example.com)" || got["format"] != nil {
							t.Fatalf("Bark did not receive plain text: %s", body)
						}
					case Telegram:
						text, _ := got["text"].(string)
						if got["parse_mode"] != "HTML" || !strings.Contains(text, "<b>site</b>") || !strings.Contains(text, `<a href="https://example.com">details</a>`) {
							t.Fatalf("incorrect Telegram HTML: %s", body)
						}
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			message := Message{Title: "result", Text: "original", Variables: map[string]string{"site": "site"}, Payload: map[string]string{"event": "site_checkin_failed", "message": "detail"}}
			if err := Deliver(context.Background(), client, target, message); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMarkdownCodeVariablesStayLiteralAndCannotCloseTheirDelimiters(t *testing.T) {
	for _, source := range []string{"`{{site}}`", "```text\n{{site}}\n```", "~~~text\n{{site}}\n~~~", "```text\n{{site}}"} {
		value := "account_*`x`<tag>&"
		if strings.Contains(source, "\n") {
			value += "\n```\n~~~\n**injected**"
		}
		message, err := applyTemplate(Template{Format: MarkdownFormat, Body: source}, Message{Text: "original", Variables: map[string]string{"site": value}})
		if err != nil {
			t.Fatal(err)
		}
		htmlBody, _, _ := markdownDocument(message.Text)
		plain, _ := markdownPlainText(message.Text)
		if plain != value || strings.Contains(htmlBody, "<strong>") || strings.Contains(htmlBody, "<tag>") || !strings.Contains(htmlBody, "<code") {
			t.Fatalf("code variable changed or escaped its code region: %q, %s", plain, htmlBody)
		}
	}
}

func TestTelegramMarkdownTruncatesDecodedTextAndClosesTags(t *testing.T) {
	text, err := telegramMarkdown("<title>&😀", "**["+strings.Repeat("😀<&", 2000)+"](https://example.com?q=a&x=b)**", 4000)
	if err != nil || !utf8.ValidString(text) || !strings.HasSuffix(text, "…") || !strings.Contains(text, "&lt;title&gt;&amp;") {
		t.Fatalf("invalid truncated Telegram message: %v", err)
	}
	decoder := xml.NewDecoder(strings.NewReader("<root>" + text + "</root>"))
	units := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("truncation left invalid HTML: %v", err)
		}
		if value, ok := token.(xml.CharData); ok {
			for _, character := range string(value) {
				units++
				if character > 0xffff {
					units++
				}
			}
		}
	}
	if units > 4000 {
		t.Fatalf("Telegram message exceeds the visible text limit: %d", units)
	}
}

func TestMarkdownUsesSupportedTelegramTagsAndRejectsUnsafeHTML(t *testing.T) {
	source := "# Heading\n\n~~gone~~ *italic* `code`\n\n- item\n- [link](javascript:alert)\n\n```go\nfmt.Println(\"hello\")\n```\n\n| A | B |\n| - | - |\n| one | two |\n\n<script>alert('bad')</script>"
	text, err := telegramMarkdown("result", source, 4000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<b>Heading</b>", "<s>gone</s>", "<i>italic</i>", "<code>code</code>", "<pre><code>", "• item", "one | two"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in Telegram message: %s", want, text)
		}
	}
	for _, forbidden := range []string{"<h1>", "<ul>", "<table>", "<script>", "javascript:", "alert('bad')"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unsupported or unsafe output %q: %s", forbidden, text)
		}
	}
}

func TestMarkdownEmailContainsPlainAndHTMLAlternatives(t *testing.T) {
	message, err := applyTemplate(Template{Format: MarkdownFormat, Body: "**{{site}}**\n[details](https://example.com)\n\n<script>unsafe</script>"}, Message{Title: "结果\r\nBcc: injected@example.com", Text: "original", Timestamp: time.Now().UTC(), Variables: map[string]string{"site": "站点 <unsafe> & *name*"}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := encodeSMTPMessage("sender@example.com", []string{"recipient@example.com"}, message)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(string(content)))
	if err != nil || parsed.Header.Get("Bcc") != "" {
		t.Fatalf("invalid or injected email headers: %v", err)
	}
	kind, parameters, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/alternative" {
		t.Fatalf("email does not contain alternatives: %v", err)
	}
	reader := multipart.NewReader(parsed.Body, parameters["boundary"])
	for index, wantKind := range []string{"text/plain", "text/html"} {
		part, err := reader.NextRawPart()
		if err != nil {
			t.Fatal(err)
		}
		kind, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || kind != wantKind || part.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
			t.Fatalf("invalid alternative %d headers: %v", index, err)
		}
		body, err := io.ReadAll(quotedprintable.NewReader(part))
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 && !strings.Contains(string(body), "站点 <unsafe> & *name*") || index == 1 && !strings.Contains(string(body), "<strong>站点 &lt;unsafe&gt; &amp; *name*</strong>") {
			t.Fatalf("variable content or formatting was lost: %s", body)
		}
		if strings.Contains(string(body), "<script>") || !strings.Contains(string(body), "https://example.com") {
			t.Fatalf("unsafe HTML or missing link in email: %s", body)
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("unexpected email part: %v", err)
	}
}
