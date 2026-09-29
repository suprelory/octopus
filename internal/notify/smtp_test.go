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

func TestSMTPDeliversUnicodeMessageToAllRecipients(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan string, 16)
	content := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
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
				var body strings.Builder
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					body.WriteString(line)
				}
				content <- body.String()
				_, _ = fmt.Fprint(conn, "250 queued\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				_, _ = fmt.Fprint(conn, "500 unsupported\r\n")
			}
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	config := Config{SMTPHost: host, SMTPPort: port, SMTPFrom: "Octopus <sender@example.com>", SMTPTo: "a@example.com, b@example.com", SMTPTLS: "none"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	err = Deliver(context.Background(), nil, config.Targets()[0], Message{Title: "签到成功\r\nBcc: injected@example.com", Text: "站点：示例\n奖励：2.5"})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	close(commands)
	var recipients []string
	for command := range commands {
		if strings.HasPrefix(command, "RCPT") {
			recipients = append(recipients, command)
		}
	}
	if len(recipients) != 2 || recipients[0] != "RCPT TO:<a@example.com>" || recipients[1] != "RCPT TO:<b@example.com>" {
		t.Fatalf("incorrect recipient envelope: %v", recipients)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(<-content))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || !strings.Contains(subject, "签到成功") || parsed.Header.Get("Bcc") != "" {
		t.Fatalf("invalid or injected subject: %q, %v", subject, err)
	}
	if _, err := mail.ParseDate(parsed.Header.Get("Date")); err != nil || parsed.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatalf("invalid email headers: %v", err)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if !strings.Contains(string(body), "奖励：2.5") {
		t.Fatal("SMTP body lost the result")
	}
}

func TestSMTPRequiresSTARTTLSByDefaultAndHonorsCancellation(t *testing.T) {
	for _, hang := range []bool{false, true} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			if hang {
				<-ctx.Done()
				return
			}
			_, _ = fmt.Fprint(conn, "220 localhost\r\n")
			_, _ = bufio.NewReader(conn).ReadString('\n')
			_, _ = fmt.Fprint(conn, "250 localhost\r\n")
		}()
		host, portText, _ := net.SplitHostPort(listener.Addr().String())
		port, _ := strconv.Atoi(portText)
		config := Config{SMTPHost: host, SMTPPort: port, SMTPFrom: "a@example.com", SMTPTo: "b@example.com"}
		started := time.Now()
		err = Deliver(ctx, nil, config.Targets()[0], Message{Title: "test"})
		cancel()
		_ = listener.Close()
		<-done
		if err == nil || time.Since(started) > time.Second {
			t.Fatalf("insecure or unbounded SMTP session: %v", err)
		}
	}
}
