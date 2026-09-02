// Package smtpoutbox is the production identity code-delivery adapter: it
// sends login and contact-channel verification codes to email addresses
// through an SMTP server using the standard library. Port 465 is dialed with
// implicit TLS (SMTPS); other ports upgrade with STARTTLS when the server
// advertises it. It mirrors the reminder module's SMTP notifier; each
// bounded context owns its adapter.
package smtpoutbox

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// fallbackTimeout bounds a send when the config carries no timeout.
const fallbackTimeout = 30 * time.Second

// implicitTLSPort is the SMTPS port: the connection is TLS-encrypted from
// the first byte, so no STARTTLS upgrade happens or is needed.
const implicitTLSPort = 465

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
	cfg Config
	// dial and dialTLS are injectable so tests never need a real SMTP
	// server; the defaults are timeout-bounded plain and TLS dials.
	dial    func(network, addr string, timeout time.Duration) (net.Conn, error)
	dialTLS func(network, addr string, timeout time.Duration) (net.Conn, error)
	// startTLS is injectable so tests can observe or stub the STARTTLS
	// upgrade without a real TLS handshake; the default is client.StartTLS.
	startTLS func(client *smtp.Client, config *tls.Config) error
}

var _ ports.MessageOutbox = (*Outbox)(nil)

// New returns an SMTP outbox for cfg.
func New(cfg Config) *Outbox {
	outbox := &Outbox{cfg: cfg, dial: net.DialTimeout}
	outbox.dialTLS = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: timeout}
		return tls.DialWithDialer(dialer, network, addr, &tls.Config{ServerName: cfg.Host})
	}
	outbox.startTLS = func(client *smtp.Client, config *tls.Config) error {
		return client.StartTLS(config)
	}
	return outbox
}

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
	var conn net.Conn
	var err error
	if o.cfg.Port == implicitTLSPort {
		conn, err = o.dialTLS("tcp", addr, timeout)
	} else {
		conn, err = o.dial("tcp", addr, timeout)
	}
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
	// The 465 connection is encrypted from the first byte; every other port
	// upgrades with STARTTLS when the server advertises it, and stays plain
	// otherwise (auth then fails closed below).
	if o.cfg.Port != implicitTLSPort {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := o.startTLS(client, &tls.Config{ServerName: o.cfg.Host}); err != nil {
				return fmt.Errorf("starttls: %v", err)
			}
		}
	}
	if o.cfg.Username != "" {
		var auth smtp.Auth
		if o.cfg.Port == implicitTLSPort {
			// smtp.PlainAuth only trusts STARTTLS-upgraded connections; the
			// implicitly-encrypted 465 path needs the local PLAIN auth.
			auth = plainAuth{username: o.cfg.Username, password: o.cfg.Password, host: o.cfg.Host}
		} else {
			auth = smtp.PlainAuth("", o.cfg.Username, o.cfg.Password, o.cfg.Host)
		}
		if err := client.Auth(auth); err != nil {
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

// plainAuth is a PLAIN smtp.Auth for the implicit-TLS (port 465) path only.
// The standard library's smtp.PlainAuth refuses to emit credentials unless
// the connection was upgraded by STARTTLS — client TLS state is set
// exclusively by StartTLS, so an implicitly-encrypted connection never
// qualifies even though it is encrypted from the first byte. This type drops
// that guard while keeping the host-name check; it is never used on a plain
// connection.
type plainAuth struct {
	username string
	password string
	host     string
}

func (a plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server.Name != a.host {
		return "", nil, fmt.Errorf("smtp: implicit-TLS PLAIN auth for %s got server %s", a.host, server.Name)
	}
	return "PLAIN", []byte("\x00" + a.username + "\x00" + a.password), nil
}

func (plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("smtp: unexpected server challenge during PLAIN auth")
	}
	return nil, nil
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
