package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

func deliverSMTP(parent context.Context, c SMTPConfig, title, message string) Result {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	failed := Result{Status: "failed", Reason: "smtp_rejected"}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return Result{Status: "failed", Reason: "smtp_unavailable"}
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var wire net.Conn = conn
	if c.TLS == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if secure.HandshakeContext(ctx) != nil {
			return Result{Status: "failed", Reason: "smtp_tls_failed"}
		}
		wire = secure
	}
	client, err := smtp.NewClient(wire, c.Host)
	if err != nil {
		return failed
	}
	defer client.Close()
	if c.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return Result{Status: "failed", Reason: "smtp_tls_failed"}
		}
		if client.StartTLS(tlsConfig) != nil {
			return Result{Status: "failed", Reason: "smtp_tls_failed"}
		}
	}
	if c.Username != "" {
		if client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)) != nil {
			return Result{Status: "failed", Reason: "smtp_auth_failed"}
		}
	}
	from, _ := mail.ParseAddress(c.From)
	if err := client.Mail(from.Address); err != nil {
		return failed
	}
	to := make([]string, 0, len(c.To))
	for _, address := range c.To {
		parsed, _ := mail.ParseAddress(address)
		if client.Rcpt(parsed.Address) != nil {
			return failed
		}
		to = append(to, parsed.String())
	}
	writer, err := client.Data()
	if err != nil {
		return failed
	}
	var data bytes.Buffer
	fmt.Fprintf(&data, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", from.String(), strings.Join(to, ",\r\n "), smtpSubject(title), time.Now().Format(time.RFC1123Z))
	encoded := quotedprintable.NewWriter(&data)
	_, _ = encoded.Write([]byte(message))
	_ = encoded.Close()
	// Once DATA starts, a disconnected socket can no longer prove rejection.
	if _, err := writer.Write(data.Bytes()); err != nil {
		return Result{Status: "uncertain", Reason: "smtp_uncertain"}
	}
	if err := writer.Close(); err != nil {
		return Result{Status: "uncertain", Reason: "smtp_uncertain"}
	}
	// The server has accepted DATA. QUIT errors must not cause a duplicate mail.
	_ = client.Quit()
	return Result{Status: "sent", Reason: "accepted"}
}

// Encode even ASCII subjects, folding at UTF-8 boundaries within RFC 2047's
// encoded-word limit. Long titles must not create overlong SMTP header lines.
func smtpSubject(title string) string {
	title = strings.Join(strings.Fields(clean(title)), " ")
	var parts []string
	chunk := ""
	for _, r := range title {
		if len(chunk)+len(string(r)) > 42 {
			parts = append(parts, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(chunk))+"?=")
			chunk = ""
		}
		chunk += string(r)
	}
	if chunk != "" {
		parts = append(parts, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(chunk))+"?=")
	}
	return strings.Join(parts, "\r\n ")
}
