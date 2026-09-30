package notify

import (
	"bytes"
	"errors"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	htmlnode "golang.org/x/net/html"
)

type Format string

const (
	TextFormat     Format = "text"
	MarkdownFormat Format = "markdown"
)

// Raw HTML stays disabled. Only template syntax supplies formatting; variable
// values are escaped before parsing and never become links or HTML elements.
var notificationMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(goldmarkhtml.WithHardWraps()),
)

func escapeMarkdown(value string) string {
	var out strings.Builder
	for _, r := range strings.ReplaceAll(value, "\r\n", "\n") {
		if r == '\x00' {
			continue
		}
		if r >= '!' && r <= '/' || r >= ':' && r <= '@' || r >= '[' && r <= '`' || r >= '{' && r <= '~' {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

func markdownDocument(source string) (string, *htmlnode.Node, error) {
	var content bytes.Buffer
	if err := notificationMarkdown.Convert([]byte(source), &content); err != nil {
		return "", nil, errors.New("could not render notification Markdown")
	}
	document, err := htmlnode.Parse(strings.NewReader(content.String()))
	if err != nil {
		return "", nil, errors.New("could not render notification Markdown")
	}
	return content.String(), document, nil
}

func markdownPlainText(source string) (string, error) {
	_, document, err := markdownDocument(source)
	if err != nil {
		return "", err
	}
	return markdownText(document), nil
}

func markdownText(document *htmlnode.Node) string {
	renderer := notificationTextRenderer{}
	renderer.render(document)
	return strings.TrimSpace(renderer.out.String())
}

func telegramMarkdown(title, source string, maxUnits int) (string, error) {
	_, document, err := markdownDocument(source)
	if err != nil {
		return "", err
	}
	// Count decoded text in UTF-16 units. Truncation happens between runes, before
	// HTML escaping; recursive rendering always closes every formatting element.
	renderer := notificationTextRenderer{rich: true, maxUnits: maxUnits - 1}
	if title != "" {
		renderer.out.WriteString("<b>")
		renderer.writeText(title)
		renderer.out.WriteString("</b>")
		renderer.writeText("\n")
	}
	renderer.render(document)
	text := strings.TrimSpace(renderer.out.String())
	if renderer.truncated {
		text += "…"
	}
	return text, nil
}

type notificationTextRenderer struct {
	out       strings.Builder
	rich      bool
	maxUnits  int
	units     int
	truncated bool
}

func (r *notificationTextRenderer) writeText(text string) {
	if r.truncated {
		return
	}
	for _, character := range text {
		width := 1
		if character > 0xffff {
			width = 2
		}
		if r.rich && r.units+width > r.maxUnits {
			r.truncated = true
			return
		}
		r.units += width
		if r.rich {
			r.out.WriteString(html.EscapeString(string(character)))
		} else {
			r.out.WriteRune(character)
		}
	}
}

func (r *notificationTextRenderer) render(node *htmlnode.Node) {
	if r.truncated {
		return
	}
	if node.Type == htmlnode.TextNode {
		// The HTML renderer adds whitespace between block elements.
		if strings.TrimSpace(node.Data) == "" && node.Parent != nil {
			switch node.Parent.Data {
			case "html", "body", "ul", "ol", "table", "thead", "tbody", "tr":
				return
			}
		}
		text := node.Data
		if node.PrevSibling != nil && node.PrevSibling.Type == htmlnode.ElementNode && node.PrevSibling.Data == "br" {
			text = strings.TrimPrefix(text, "\n") // Goldmark adds a newline after <br>.
		}
		r.writeText(text)
		return
	}
	if node.Type == htmlnode.CommentNode {
		return
	}
	var tag, ending string
	if node.Type == htmlnode.ElementNode {
		switch node.Data {
		case "strong", "b", "h1", "h2", "h3", "h4", "h5", "h6":
			tag = "b"
		case "em", "i":
			tag = "i"
		case "del", "s":
			tag = "s"
		case "code", "pre":
			tag = node.Data
		case "a":
			href := nodeAttribute(node, "href")
			if r.rich && safeTelegramLink(href) {
				r.out.WriteString("<a href=\"" + html.EscapeString(href) + "\">")
				ending = "</a>"
			} else if !r.rich && href != "" {
				ending = " (" + href + ")"
			}
		case "br":
			r.writeText("\n")
			return
		case "hr":
			r.writeText("\n────────\n")
			return
		case "img":
			r.writeText(nodeAttribute(node, "alt"))
			return
		case "input":
			if nodeAttribute(node, "type") == "checkbox" {
				check := "[ ] "
				for _, attribute := range node.Attr {
					if attribute.Key == "checked" {
						check = "[x] "
					}
				}
				r.writeText(check)
			}
			return
		case "li":
			prefix := "• "
			if node.Parent != nil && node.Parent.Data == "ol" {
				number, err := strconv.Atoi(nodeAttribute(node.Parent, "start"))
				if err != nil {
					number = 1
				}
				for previous := node.PrevSibling; previous != nil; previous = previous.PrevSibling {
					if previous.Type == htmlnode.ElementNode && previous.Data == "li" {
						number++
					}
				}
				prefix = strconv.Itoa(number) + ". "
			}
			r.writeText(prefix)
		case "td", "th":
			for previous := node.PrevSibling; previous != nil; previous = previous.PrevSibling {
				if previous.Type == htmlnode.ElementNode {
					r.writeText(" | ")
					break
				}
			}
		}
	}
	if r.rich && tag != "" {
		r.out.WriteString("<" + tag + ">")
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		r.render(child)
		if r.truncated {
			break
		}
	}
	if r.rich && tag != "" {
		r.out.WriteString("</" + tag + ">")
	}
	if r.rich {
		r.out.WriteString(ending)
	} else {
		r.writeText(ending)
	}
	switch node.Data {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote", "ul", "ol", "table":
		r.writeText("\n\n")
	case "li", "tr":
		r.writeText("\n")
	}
}

func nodeAttribute(node *htmlnode.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func safeTelegramLink(href string) bool {
	link, err := url.Parse(href)
	if err != nil {
		return false
	}
	switch strings.ToLower(link.Scheme) {
	case "http", "https":
		return link.Hostname() != ""
	case "mailto":
		return link.Opaque != "" || link.Path != ""
	}
	return false
}
