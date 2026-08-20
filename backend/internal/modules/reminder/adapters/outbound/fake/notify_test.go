package fake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

var (
	_ ports.EmailNotifier = (*Notifier)(nil)
	_ ports.SmsNotifier   = (*Notifier)(nil)
)

// recordedWrite is one captured outbox.Write call.
type recordedWrite struct {
	channel string
	address string
	todoID  string
	body    string
}

// recordingOutbox captures Write calls instead of touching a database.
type recordingOutbox struct {
	writes []recordedWrite
	err    error
}

func (r *recordingOutbox) Write(_ context.Context, channel, address, todoID, body string) error {
	r.writes = append(r.writes, recordedWrite{channel, address, todoID, body})
	return r.err
}

func testMessage() dto.ReminderMessage {
	return dto.ReminderMessage{
		To:             "alice@example.com",
		TodoID:         "todo-123",
		Title:          "提交周报",
		ScheduledAtUTC: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
	}
}

func TestConstructorsSetDistinctChannels(t *testing.T) {
	email := NewEmail(nil)
	sms := NewSms(nil)
	if email.channel == sms.channel {
		t.Fatalf("email channel = %q = sms channel, want distinct", email.channel)
	}
	if email.channel != "email" || sms.channel != "sms" {
		t.Fatalf("channels = %q/%q, want email/sms", email.channel, sms.channel)
	}
}

func TestEmailNotifierRendersTemplateAndWritesOutbox(t *testing.T) {
	outbox := &recordingOutbox{}
	notifier := &Notifier{channel: "email", outbox: outbox}

	result, err := notifier.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(outbox.writes) != 1 {
		t.Fatalf("outbox writes = %d, want 1", len(outbox.writes))
	}
	write := outbox.writes[0]
	if write.channel != "email" {
		t.Fatalf("write channel = %q, want email", write.channel)
	}
	if write.address != "alice@example.com" || write.todoID != "todo-123" {
		t.Fatalf("write = %#v, want address alice@example.com todo todo-123", write)
	}
	if !strings.Contains(write.body, "《提交周报》") {
		t.Fatalf("body %q does not contain the 《title》", write.body)
	}
	if !strings.Contains(write.body, "2026-08-20T08:00:00Z") {
		t.Fatalf("body %q does not contain the UTC instant", write.body)
	}
	if !strings.HasPrefix(result.ProviderMessageID, "fake-") {
		t.Fatalf("ProviderMessageID = %q, want fake- prefix", result.ProviderMessageID)
	}
}

func TestSmsNotifierWritesSmsChannel(t *testing.T) {
	outbox := &recordingOutbox{}
	notifier := &Notifier{channel: "sms", outbox: outbox}

	result, err := notifier.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(outbox.writes) != 1 || outbox.writes[0].channel != "sms" {
		t.Fatalf("outbox writes = %#v, want one sms write", outbox.writes)
	}
	if !strings.Contains(outbox.writes[0].body, "《提交周报》") || !strings.Contains(outbox.writes[0].body, "2026-08-20T08:00:00Z") {
		t.Fatalf("body %q must contain the 《title》 and UTC instant", outbox.writes[0].body)
	}
	if !strings.HasPrefix(result.ProviderMessageID, "fake-") {
		t.Fatalf("ProviderMessageID = %q, want fake- prefix", result.ProviderMessageID)
	}
}

func TestFakeMessageIDIsDeterministic(t *testing.T) {
	notifier := &Notifier{channel: "email", outbox: &recordingOutbox{}}
	first, err := notifier.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	second, err := notifier.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if first.ProviderMessageID != second.ProviderMessageID {
		t.Fatalf("message ids = %q then %q, want identical for the same message", first.ProviderMessageID, second.ProviderMessageID)
	}
	if len(first.ProviderMessageID) != len("fake-")+16 {
		t.Fatalf("message id %q, want fake- plus 16 hex chars", first.ProviderMessageID)
	}

	other := testMessage()
	other.TodoID = "todo-999"
	third, err := notifier.Send(context.Background(), other)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if third.ProviderMessageID == first.ProviderMessageID {
		t.Fatalf("message id %q reused for a different todo", third.ProviderMessageID)
	}
}

func TestNotifierSurfacesFailError(t *testing.T) {
	outbox := &recordingOutbox{}
	failErr := errors.New("injected provider failure")
	notifier := &Notifier{channel: "email", outbox: outbox, FailError: failErr}

	_, err := notifier.Send(context.Background(), testMessage())
	if !errors.Is(err, failErr) {
		t.Fatalf("Send() error = %v, want the injected FailError", err)
	}
	if len(outbox.writes) != 0 {
		t.Fatalf("outbox writes = %d, want none when failing", len(outbox.writes))
	}
}

func TestNotifierSurfacesOutboxError(t *testing.T) {
	writeErr := errors.New("outbox insert failed")
	notifier := &Notifier{channel: "sms", outbox: &recordingOutbox{err: writeErr}}

	_, err := notifier.Send(context.Background(), testMessage())
	if !errors.Is(err, writeErr) {
		t.Fatalf("Send() error = %v, want the outbox write error", err)
	}
}

func TestNotifierDelayDoesNotBlockSuccess(t *testing.T) {
	outbox := &recordingOutbox{}
	notifier := &Notifier{channel: "email", outbox: outbox, Delay: time.Millisecond}

	if _, err := notifier.Send(context.Background(), testMessage()); err != nil {
		t.Fatalf("Send() with small Delay error = %v", err)
	}
	if len(outbox.writes) != 1 {
		t.Fatalf("outbox writes = %d, want 1", len(outbox.writes))
	}
}
