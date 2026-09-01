package smtpoutbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// scriptedConn is a fake net.Conn ported from the reminder module's SMTP
// notifier test. The reminder test uses a real TCP listener that feeds
// scripted responses; here we collapse the same idea into an in-process
// fake conn: responses are pre-loaded line by line and served via Read,
// while every byte the client writes is captured for assertions.
type scriptedConn struct {
	mu        sync.Mutex
	responses []byte
	written   bytes.Buffer
}

func newScriptedConn(lines []string) *scriptedConn {
	var buf bytes.Buffer
	for _, l := range lines {
		buf.WriteString(l)
	}
	return &scriptedConn{responses: buf.Bytes()}
}

func (c *scriptedConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return 0, io.EOF
	}
	n := copy(b, c.responses)
	c.responses = c.responses[n:]
	return n, nil
}

func (c *scriptedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written.Write(b)
}

func (c *scriptedConn) Close() error { return nil }

func (c *scriptedConn) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (c *scriptedConn) SetDeadline(t time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *scriptedConn) transcript() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.written.String()
}

func TestWriteSendsLoginCodeEmail(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n", // EHLO reply (no AUTH capability)
		"250 OK\r\n",               // MAIL FROM
		"250 OK\r\n",               // RCPT TO
		"354 End data with <CR LF>.<CR LF>\r\n",
		"250 queued\r\n",
		"221 bye\r\n",
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "admin@example.com", Channel: "email", Purpose: "login", Code: "123456",
	})
	if err != nil {
		t.Fatalf("Write = %v", err)
	}
	transcript := conn.transcript()
	for _, want := range []string{"MAIL FROM:<noreply@example.com>", "RCPT TO:<admin@example.com>", "Subject: 登录验证码", "123456"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("transcript missing %q:\n%s", want, transcript)
		}
	}
}

func TestWriteRejectsNonEmailMessage(t *testing.T) {
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com"})
	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "+8613800138000", Channel: "sms", Purpose: "login", Code: "123456",
	})
	if !errors.Is(err, domain.ErrSmsUnavailable) {
		t.Fatalf("Write = %v, want ErrSmsUnavailable", err)
	}
}

func TestWritePermanentRefusalWrapsDeliveryFailed(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n",
		"550 mailbox unavailable\r\n", // MAIL FROM refused
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "admin@example.com", Channel: "email", Purpose: "login", Code: "123456",
	})
	if !errors.Is(err, domain.ErrCodeDeliveryFailed) {
		t.Fatalf("Write = %v, want ErrCodeDeliveryFailed", err)
	}
}
