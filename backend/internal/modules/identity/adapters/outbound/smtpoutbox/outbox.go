// Package smtpoutbox is the production identity code-delivery adapter: it
// sends login and contact-channel verification codes to email addresses
// through a plain SMTP server using the standard library. It mirrors the
// reminder module's SMTP notifier; each bounded context owns its adapter.
package smtpoutbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// fallbackTimeout bounds a send when the config carries no timeout.
const fallbackTimeout = 30 * time.Second

// Config carries the SMTP endpoint and credentials. Auth is PLAIN and only
// attempted when Username is set.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

// Outbox implements ports.MessageOutbox over SMTP for email messages.
type Outbox struct {
	cfg  Config
	dial func(network, addr string, timeout time.Duration) (net.Conn, error)
}

var _ ports.MessageOutbox = (*Outbox)(nil)

// New returns an SMTP outbox for cfg.
func New(cfg Config) *Outbox { return &Outbox{cfg: cfg, dial: net.DialTimeout} }

// Write delivers one verification code email. A non-email message fails
// closed with ErrSmsUnavailable; every SMTP or transport failure wraps
// domain.ErrCodeDeliveryFailed so the HTTP layer reports one stable code.
func (o *Outbox) Write(ctx context.Context, message ports.OutboxMessage) error {
	if message.Channel != "email" {
		return domain.ErrSmsUnavailable
	}
	err := o.send(ctx, message)
	if err == nil || errors.Is(err, domain.ErrCodeDeliveryFailed) {
		return err
	}
	return fmt.Errorf("%w: %v", domain.ErrCodeDeliveryFailed, err)
}

func (o *Outbox) send(ctx context.Context, message ports.OutboxMessage) error {
	timeout := o.cfg.Timeout
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	addr := net.JoinHostPort(o.cfg.Host, strconv.Itoa(o.cfg.Port))
	conn, err := o.dial("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	// Bound the whole conversation by both the configured timeout and the
	// caller's context deadline, whichever ends first.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %v", err)
	}

	client, err := smtp.NewClient(conn, o.cfg.Host)
	if err != nil {
		return fmt.Errorf("handshake %s: %v", addr, err)
	}
	defer client.Close()
	if o.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", o.cfg.Username, o.cfg.Password, o.cfg.Host)); err != nil {
			return fmt.Errorf("auth: %v", err)
		}
	}
	if err := client.Mail(o.cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt(message.Address); err != nil {
		return fmt.Errorf("RCPT TO: %v", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %v", err)
	}
	if _, err := writer.Write(renderCodeMessage(o.cfg.From, message)); err != nil {
		writer.Close()
		return fmt.Errorf("write message: %v", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("end of DATA: %v", err)
	}
	// The message is queued; a failed QUIT must not fail the send.
	_ = client.Quit()
	return nil
}

// renderCodeMessage renders the RFC 5322 message; the subject reflects the
// code purpose so recipients can tell login codes from channel codes.
func renderCodeMessage(from string, message ports.OutboxMessage) []byte {
	subject := "验证码"
	if message.Purpose == "login" {
		subject = "登录验证码"
	}
	return []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n验证码：%s\r\n如非本人操作，请忽略本邮件。\r\n",
		from, message.Address, subject, time.Now().UTC().Format(time.RFC1123Z), message.Code,
	))
}

// IsPermanent reports whether the underlying SMTP refusal was a 5xx, for
// logging; delivery semantics stay uniform for the caller.
func IsPermanent(err error) bool {
	var protoErr *textproto.Error
	return errors.As(err, &protoErr) && protoErr.Code >= 500 && protoErr.Code <= 599
}
