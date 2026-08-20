package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

// Channel values recorded into reminder.fake_outbox; they mirror the dispatch
// keys in the send command instead of importing a shared constant package.
const (
	channelEmail = "email"
	channelSMS   = "sms"
)

// subject headlines every fake reminder so the dev inbox renders a stable,
// recognizable sender identity.
const subject = "工作台提醒"

// outboxWriter is the narrow slice of the fake Outbox the notifiers need;
// keeping it as a package-local interface lets notifier tests record writes
// without a database. *Outbox satisfies it.
type outboxWriter interface {
	Write(ctx context.Context, channel, address, todoID, body string) error
}

// Notifier renders one reminder into the fake outbox instead of calling a
// real provider. The zero-valued knobs are the wired defaults; tests set
// FailError/Delay to exercise the worker's failure and latency paths.
type Notifier struct {
	channel string
	outbox  outboxWriter

	// FailError, when non-nil, is returned instead of writing to the outbox.
	FailError error
	// Delay, when non-zero, is waited before the outbox write.
	Delay time.Duration
}

var (
	_ ports.EmailNotifier = (*Notifier)(nil)
	_ ports.SmsNotifier   = (*Notifier)(nil)
)

// NewEmail returns the fake email notifier writing to outbox.
func NewEmail(outbox *Outbox) *Notifier { return &Notifier{channel: channelEmail, outbox: outbox} }

// NewSms returns the fake SMS notifier writing to outbox.
func NewSms(outbox *Outbox) *Notifier { return &Notifier{channel: channelSMS, outbox: outbox} }

// Send renders the reminder and records it in the fake outbox under the
// notifier's channel, or fails without writing when FailError is set.
func (n *Notifier) Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	if n.Delay > 0 {
		select {
		case <-time.After(n.Delay):
		case <-ctx.Done():
			return dto.SendResult{}, ctx.Err()
		}
	}
	if n.FailError != nil {
		return dto.SendResult{}, n.FailError
	}
	body := renderBody(n.channel, message)
	if err := n.outbox.Write(ctx, n.channel, message.To, message.TodoID, body); err != nil {
		return dto.SendResult{}, err
	}
	return dto.SendResult{ProviderMessageID: messageID(n.channel, message)}, nil
}

// renderBody renders the fixed fake template: the subject, the 《title》, and
// the scheduled instant in UTC. Email folds the subject into the body's first
// line because the outbox row stores the body only; SMS wraps it in 【】.
func renderBody(channel string, message dto.ReminderMessage) string {
	instant := message.ScheduledAtUTC.UTC().Format(time.RFC3339)
	if channel == channelSMS {
		return fmt.Sprintf("【%s】《%s》提醒时间：%s", subject, message.Title, instant)
	}
	return fmt.Sprintf("%s\n\n《%s》\n提醒时间：%s", subject, message.Title, instant)
}

// messageID derives a deterministic provider message id ("fake-" + 16 hex
// chars) from the channel and the message's identity, so retried attempts of
// the same send report the same id.
func messageID(channel string, message dto.ReminderMessage) string {
	sum := sha256.Sum256([]byte(channel + "\x00" + message.To + "\x00" + message.TodoID))
	return "fake-" + hex.EncodeToString(sum[:8])
}
