// Package smtp is the ITER-0003 real email notifier behind
// ports.EmailNotifier: it submits one reminder through an SMTP server using
// the standard library. Port 465 is dialed with implicit TLS (SMTPS); other
// ports upgrade with STARTTLS when the server advertises it. 5xx protocol
// refusals are permanent; 4xx, dial, TLS, and deadline failures are
// transient and retried by the queue.
package smtp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

// subject headlines every reminder email.
const subject = "工作台提醒"

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

// Notifier submits reminder emails over SMTP.
type Notifier struct {
	cfg Config
	// dial and dialTLS are injectable so tests never need a real SMTP
	// server; the defaults are timeout-bounded plain and TLS dials.
	dial    func(network, addr string, timeout time.Duration) (net.Conn, error)
	dialTLS func(network, addr string, timeout time.Duration) (net.Conn, error)
	// startTLS is injectable so tests can observe or stub the STARTTLS
	// upgrade without a real TLS handshake; the default is client.StartTLS.
	startTLS func(client *smtp.Client, config *tls.Config) error
}

var _ ports.EmailNotifier = (*Notifier)(nil)

// New returns an SMTP notifier for cfg.
func New(cfg Config) *Notifier {
	notifier := &Notifier{cfg: cfg, dial: net.DialTimeout}
	notifier.dialTLS = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: timeout}
		return tls.DialWithDialer(dialer, network, addr, &tls.Config{ServerName: cfg.Host})
	}
	notifier.startTLS = func(client *smtp.Client, config *tls.Config) error {
		return client.StartTLS(config)
	}
	return notifier
}

// Send delivers one reminder email. SMTP 5xx refusals are returned as
// *ports.PermanentError carrying the reply code; 4xx replies, dial, TLS, and
// deadline failures are plain transient errors.
func (n *Notifier) Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	timeout := n.cfg.Timeout
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	addr := net.JoinHostPort(n.cfg.Host, strconv.Itoa(n.cfg.Port))
	var conn net.Conn
	var err error
	if n.cfg.Port == implicitTLSPort {
		conn, err = n.dialTLS("tcp", addr, timeout)
	} else {
		conn, err = n.dial("tcp", addr, timeout)
	}
	if err != nil {
		return dto.SendResult{}, fmt.Errorf("smtp: dial %s: %w", addr, err)
	}
	defer conn.Close()
	// Bound the whole conversation by both the configured timeout and the
	// caller's context deadline, whichever ends first.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return dto.SendResult{}, fmt.Errorf("smtp: set deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		return dto.SendResult{}, classify(fmt.Errorf("smtp: handshake %s: %w", addr, err))
	}
	defer client.Close()
	// The 465 connection is encrypted from the first byte; every other port
	// upgrades with STARTTLS when the server advertises it, and stays plain
	// otherwise (auth then fails closed below).
	if n.cfg.Port != implicitTLSPort {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := n.startTLS(client, &tls.Config{ServerName: n.cfg.Host}); err != nil {
				return dto.SendResult{}, classify(fmt.Errorf("smtp: starttls: %w", err))
			}
		}
	}
	if n.cfg.Username != "" {
		var auth smtp.Auth
		if n.cfg.Port == implicitTLSPort {
			// smtp.PlainAuth only trusts STARTTLS-upgraded connections; the
			// implicitly-encrypted 465 path needs the local PLAIN auth.
			auth = plainAuth{username: n.cfg.Username, password: n.cfg.Password, host: n.cfg.Host}
		} else {
			auth = smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)
		}
		if err := client.Auth(auth); err != nil {
			return dto.SendResult{}, classify(fmt.Errorf("smtp: auth: %w", err))
		}
	}
	if err := client.Mail(n.cfg.From); err != nil {
		return dto.SendResult{}, classify(fmt.Errorf("smtp: MAIL FROM: %w", err))
	}
	if err := client.Rcpt(message.To); err != nil {
		return dto.SendResult{}, classify(fmt.Errorf("smtp: RCPT TO: %w", err))
	}
	writer, err := client.Data()
	if err != nil {
		return dto.SendResult{}, classify(fmt.Errorf("smtp: DATA: %w", err))
	}
	if _, err := writer.Write(renderMessage(n.cfg.From, message)); err != nil {
		writer.Close()
		return dto.SendResult{}, fmt.Errorf("smtp: write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return dto.SendResult{}, classify(fmt.Errorf("smtp: end of DATA: %w", err))
	}
	// The message is queued; a failed QUIT must not fail the send.
	_ = client.Quit()

	id, err := messageID()
	if err != nil {
		return dto.SendResult{}, fmt.Errorf("smtp: generate message id: %w", err)
	}
	return dto.SendResult{ProviderMessageID: id}, nil
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

// classify wraps 5xx SMTP replies into a permanent error carrying the reply
// code; everything else (4xx, transport, TLS, deadline) stays transient.
func classify(err error) error {
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) && protoErr.Code >= 500 && protoErr.Code <= 599 {
		return &ports.PermanentError{Code: strconv.Itoa(protoErr.Code), Cause: err}
	}
	return err
}

// renderMessage renders the RFC 5322 message: From/To/Subject/Date headers
// and a body carrying the 《title》 and the scheduled instant in UTC.
func renderMessage(from string, message dto.ReminderMessage) []byte {
	return []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n《%s》\r\n提醒时间：%s\r\n",
		from,
		message.To,
		subject,
		time.Now().UTC().Format(time.RFC1123Z),
		message.Title,
		message.ScheduledAtUTC.UTC().Format(time.RFC3339),
	))
}

// messageID generates the provider message id reported for accepted sends
// ("smtp-" + 16 hex chars); SMTP itself returns no stable id.
func messageID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "smtp-" + hex.EncodeToString(random), nil
}
