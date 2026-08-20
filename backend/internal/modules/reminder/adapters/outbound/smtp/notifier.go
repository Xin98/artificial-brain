// Package smtp is the ITER-0003 real email notifier behind
// ports.EmailNotifier: it submits one reminder through a plain SMTP server
// using the standard library. 5xx protocol refusals are permanent; 4xx, dial,
// and deadline failures are transient and retried by the queue.
package smtp

import (
	"context"
	"crypto/rand"
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
	// dial is injectable so tests never need a real SMTP server; the default
	// is a plain timeout-bounded TCP dial.
	dial func(network, addr string, timeout time.Duration) (net.Conn, error)
}

var _ ports.EmailNotifier = (*Notifier)(nil)

// New returns an SMTP notifier for cfg.
func New(cfg Config) *Notifier {
	return &Notifier{cfg: cfg, dial: net.DialTimeout}
}

// Send delivers one reminder email. SMTP 5xx refusals are returned as
// *ports.PermanentError carrying the reply code; 4xx replies, dial failures,
// and deadline overruns are plain transient errors.
func (n *Notifier) Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	timeout := n.cfg.Timeout
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	addr := net.JoinHostPort(n.cfg.Host, strconv.Itoa(n.cfg.Port))
	conn, err := n.dial("tcp", addr, timeout)
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
	if n.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)); err != nil {
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

// classify wraps 5xx SMTP replies into a permanent error carrying the reply
// code; everything else (4xx, transport, deadline) stays transient.
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
