package notify

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Template uses literal {{variable}} substitutions, without executable expressions.
// Empty fields keep the event source's original title or message.
type Template struct {
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	Format Format `json:"format,omitempty"`
}

var templateVariables = map[string]bool{
	"title": true, "message": true, "time": true, "level": true, "emoji": true,
	"event": true, "site": true, "account": true, "source": true, "detail": true,
	"reward": true, "balance": true, "threshold": true, "failure_count": true,
}

func (t Template) Validate() error {
	if t.Format != "" && t.Format != TextFormat && t.Format != MarkdownFormat {
		return errors.New("notification template format must be text or markdown")
	}
	if len(t.Title) > 512 || len(t.Body) > 8192 || strings.ContainsAny(t.Title, "\r\n\x00") || strings.ContainsRune(t.Body, '\x00') {
		return errors.New("notification template title must be a single line up to 512 bytes and body up to 8192 bytes")
	}
	for _, value := range []string{t.Title, t.Body} {
		if _, err := renderTemplate(value, nil); err != nil {
			return err
		}
	}
	return nil
}

func renderTemplate(source string, values map[string]string) (string, error) {
	var out strings.Builder
	for {
		start := strings.Index(source, "{{")
		if start < 0 {
			out.WriteString(source)
			return out.String(), nil
		}
		out.WriteString(source[:start])
		source = source[start+2:]
		end := strings.Index(source, "}}")
		if end < 0 {
			return "", errors.New("notification template contains an unclosed variable")
		}
		key := strings.TrimSpace(source[:end])
		if !templateVariables[key] {
			return "", errors.New("notification template contains an unsupported variable")
		}
		out.WriteString(values[key])
		source = source[end+2:]
	}
}

func applyTemplate(template Template, message Message) (Message, error) {
	if err := template.Validate(); err != nil {
		return Message{}, err
	}
	if strings.TrimSpace(template.Title) == "" && strings.TrimSpace(template.Body) == "" && template.Format != MarkdownFormat {
		return message, nil
	}
	values := make(map[string]string, len(templateVariables))
	for key, value := range message.Variables {
		if templateVariables[key] {
			values[key] = value
		}
	}
	values["title"], values["message"] = message.Title, message.Text
	values["time"], values["level"] = message.Timestamp.Format(time.RFC3339), message.Level
	switch message.Level {
	case "error":
		values["emoji"] = "❌"
	case "warning":
		values["emoji"] = "⚠️"
	default:
		values["emoji"] = "✅"
	}
	if values["event"] == "" {
		values["event"] = message.Title
	}
	customTitle, customBody := false, false
	if strings.TrimSpace(template.Title) != "" {
		text, _ := renderTemplate(template.Title, values)
		// Values such as site/account names can contain line breaks.
		text = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "\x00", "").Replace(text))
		if text != "" {
			message.Title = text
			customTitle = true
		}
	}
	if strings.TrimSpace(template.Body) != "" {
		text, _ := renderTemplate(template.Body, values)
		if template.Format == MarkdownFormat {
			text = renderMarkdownTemplate(template.Body, values)
		}
		if strings.TrimSpace(text) != "" {
			message.Text = text
			customBody = true
		}
	}
	if template.Format == MarkdownFormat {
		message.Format = MarkdownFormat
		if !customBody {
			message.Text = escapeMarkdown(message.Text)
		}
	}
	if message.Payload != nil {
		// Preserve structured webhook metadata while replacing only its display
		// fields. JSON encoding escapes variable values, including quotes/newlines.
		encoded, err := json.Marshal(message.Payload)
		if err != nil {
			return Message{}, errors.New("could not encode notification template payload")
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &payload); err != nil || payload == nil {
			return Message{}, errors.New("notification template payload must be an object")
		}
		if customTitle {
			payload["title"], _ = json.Marshal(message.Title)
		}
		if customBody {
			payload["message"], _ = json.Marshal(message.Text)
		} else if message.Format == MarkdownFormat {
			// A legacy webhook's message can differ from the full notification text.
			var original string
			if json.Unmarshal(payload["message"], &original) == nil {
				payload["message"], _ = json.Marshal(escapeMarkdown(original))
			}
		}
		if message.Format == MarkdownFormat {
			payload["format"], _ = json.Marshal(MarkdownFormat)
		}
		message.Payload = payload
	}
	return message, nil
}
