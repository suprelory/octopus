package notify

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"
)

func smtpSink(t *testing.T, mode string) (SMTPConfig, <-chan string, <-chan string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	commands, content, done := make(chan string, 32), make(chan string, 1), make(chan struct{})
	t.Cleanup(func() { _ = listener.Close(); <-done })
	go func() {
		defer close(done)
		defer close(commands)
		defer close(content)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if mode == "hang" {
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		_, _ = fmt.Fprint(conn, "220 localhost test SMTP\r\n")
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			commands <- strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				_, _ = fmt.Fprint(conn, "250 localhost\r\n")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			case strings.HasPrefix(line, "DATA"):
				_, _ = fmt.Fprint(conn, "354 continue\r\n")
				var data strings.Builder
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					data.WriteString(line)
				}
				content <- data.String()
				if mode == "drop-data" {
					return
				}
				_, _ = fmt.Fprint(conn, "250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				return // Even a lost QUIT response must not undo DATA acceptance.
			default:
				_, _ = fmt.Fprint(conn, "500 unsupported\r\n")
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return SMTPConfig{Host: host, Port: port, TLS: "none", From: "Octopus <a@example.com>", To: []string{"b@example.com", "c@example.com"}}, commands, content, done
}

func TestSMTPUnicodeRecipientsAndAcceptedDATA(t *testing.T) {
	c, commands, content, done := smtpSink(t, "drop-quit")
	title := strings.Repeat("签到成功😀 ", 30) + "\r\nBcc: injected@example.com"
	result := Deliver(context.Background(), nil, "smtp", Connection{SMTP: &c}, title, "站点：示例\n奖励：0.000001", nil, time.Now())
	if result.Status != "sent" {
		t.Fatalf("accepted DATA was lost: %+v", result)
	}
	<-done
	recipients := 0
	for command := range commands {
		if strings.HasPrefix(command, "RCPT TO:") {
			recipients++
		}
	}
	if recipients != 2 {
		t.Fatal("not all recipients were included")
	}
	data := <-content
	for _, line := range strings.Split(data, "\r\n") {
		if len(line) > 998 {
			t.Fatal("SMTP line exceeded RFC limit")
		}
	}
	parsed, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != strings.Join(strings.Fields(title), " ") || parsed.Header.Get("Bcc") != "" {
		t.Fatal("Unicode title changed or injected a header", err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if !strings.Contains(string(body), "奖励：0.000001") {
		t.Fatal("Unicode body changed")
	}
}

func TestSMTPNoDowngradeAfterSTARTTLSFailure(t *testing.T) {
	c, commands, _, done := smtpSink(t, "no-tls")
	c.TLS, c.Username, c.Password = "starttls", "private-user", "private-password"
	result := Deliver(context.Background(), nil, "smtp", Connection{SMTP: &c}, "title", "body", nil, time.Now())
	if result.Status != "failed" || result.Reason != "smtp_tls_failed" {
		t.Fatalf("STARTTLS failure was ignored: %+v", result)
	}
	<-done
	for command := range commands {
		if strings.HasPrefix(command, "AUTH") || strings.HasPrefix(command, "MAIL") {
			t.Fatal("SMTP fell back to plaintext authentication/delivery")
		}
	}
}

func TestSMTPInterruptedDATAIsUncertainAndCancellationIsBounded(t *testing.T) {
	c, _, _, _ := smtpSink(t, "drop-data")
	result := Deliver(context.Background(), nil, "smtp", Connection{SMTP: &c}, "title", "body", nil, time.Now())
	if result.Status != "uncertain" || result.Reason != "smtp_uncertain" {
		t.Fatal("interrupted DATA could be replayed automatically")
	}
	hanging, _, _, _ := smtpSink(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	result = Deliver(ctx, nil, "smtp", Connection{SMTP: &hanging}, "title", "body", nil, time.Now())
	if result.Status == "sent" || time.Since(started) > 2*time.Second {
		t.Fatal("SMTP ignored context cancellation")
	}
}
