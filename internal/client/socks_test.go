package client

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestSOCKSDialCancellationClosesHandshake(t *testing.T) {
	for _, stage := range []string{"greeting", "authentication", "connect"} {
		t.Run(stage, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ready := make(chan error, 1)
			closed := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					ready <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				header := make([]byte, 2)
				if _, err = io.ReadFull(conn, header); err == nil {
					_, err = io.CopyN(io.Discard, conn, int64(header[1]))
				}
				if err == nil && stage != "greeting" {
					method := byte(0)
					if stage == "authentication" {
						method = 2
					}
					_, err = conn.Write([]byte{5, method})
					if err == nil && stage == "authentication" {
						_, err = io.ReadFull(conn, header)
						if err == nil {
							_, err = io.CopyN(io.Discard, conn, int64(header[1]))
						}
						if err == nil {
							_, err = io.ReadFull(conn, header[:1])
						}
						if err == nil {
							_, err = io.CopyN(io.Discard, conn, int64(header[0]))
						}
					} else if err == nil {
						// CONNECT to the fixed IPv4 destination below.
						_, err = io.CopyN(io.Discard, conn, 10)
					}
				}
				ready <- err
				_, err = conn.Read(header)
				closed <- err
			}()
			client, err := newHTTPClientCustomProxy("socks5://user:pass@" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				conn, err := client.Transport.(*http.Transport).DialContext(ctx, "tcp", "127.0.0.1:80")
				if conn != nil {
					conn.Close()
				}
				done <- err
			}()
			select {
			case err := <-ready:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("handshake not reached")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled dial succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("dial ignored cancellation")
			}
			select {
			case err := <-closed:
				if err != io.EOF {
					t.Fatalf("connection not closed: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("proxy connection leaked")
			}
		})
	}
}
