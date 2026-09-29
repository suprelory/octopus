package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

func deliverSMTP(ctx context.Context, config Config, message Message) error {
	mode := config.SMTPTLS
	if mode == "" {
		mode = "starttls"
		if config.SMTPPort == 465 {
			mode = "tls"
		}
	}
	port := config.SMTPPort
	if port == 0 {
		port = 587
		if mode == "tls" {
			port = 465
		}
	}
	from, err := mail.ParseAddress(config.SMTPFrom)
	if err != nil {
		return errors.New("invalid SMTP sender")
	}
	to, err := mail.ParseAddressList(config.SMTPTo)
	if err != nil || len(to) == 0 {
		return errors.New("invalid SMTP recipients")
	}
	dialer := net.Dialer{Timeout: DeliveryTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(config.SMTPHost, strconv.Itoa(port)))
	if err != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	tlsConfig := &tls.Config{ServerName: config.SMTPHost, MinVersion: tls.VersionTLS12}
	var smtpConn net.Conn = conn
	if mode == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return errors.New("SMTP TLS handshake failed")
		}
		smtpConn = secure
	}
	client, err := smtp.NewClient(smtpConn, config.SMTPHost)
	if err != nil {
		return errors.New("SMTP greeting failed")
	}
	defer client.Close()
	if mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP STARTTLS failed")
		}
	}
	if config.SMTPUser != "" {
		if mode == "none" {
			return errors.New("SMTP authentication requires TLS")
		}
		if err := client.Auth(smtp.PlainAuth("", config.SMTPUser, config.SMTPPassword, config.SMTPHost)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return errors.New("SMTP sender rejected")
	}
	recipients := make([]string, 0, len(to))
	for _, recipient := range to {
		if err := client.Rcpt(recipient.Address); err != nil {
			return errors.New("SMTP recipient rejected")
		}
		recipients = append(recipients, recipient.String())
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP message rejected")
	}
	title := strings.Join(strings.Fields(message.Title), " ")
	body := strings.ReplaceAll(strings.ReplaceAll(message.Text, "\r\n", "\n"), "\n", "\r\n")
	// Quoted-printable keeps Unicode results compatible with SMTP relays that
	// do not advertise 8BITMIME, and bounds the length of each body line.
	var encodedBody bytes.Buffer
	encoder := quotedprintable.NewWriter(&encodedBody)
	_, _ = encoder.Write([]byte(body))
	_ = encoder.Close()
	content := "From: " + from.String() + "\r\nTo: " + strings.Join(recipients, ", ") +
		"\r\nSubject: " + mime.QEncoding.Encode("UTF-8", "[Octopus] "+title) +
		"\r\nDate: " + message.Timestamp.Format(time.RFC1123Z) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" + encodedBody.String() + "\r\n"
	if _, err := writer.Write([]byte(content)); err != nil {
		return errors.New("SMTP message write failed")
	}
	if err := writer.Close(); err != nil {
		return errors.New("SMTP message not accepted")
	}
	// DATA was accepted; a failed QUIT must not cause a duplicate delivery.
	_ = client.Quit()
	return nil
}
