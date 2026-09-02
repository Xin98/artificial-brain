package main

import (
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
)

func TestRunReturnsFailureForMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

// The reminder queue cases set a parseable DATABASE_URL so configuration
// loading passes the connection-string check and actually reaches the
// reminder validations; the unreachable host guarantees run() never blocks
// on a real database even when the reminder fields are valid.
func TestRunReturnsFailureForInvalidReminderQueueEmailConcurrency(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://worker@localhost:1/none")
	t.Setenv("APP_ENV", "development")
	t.Setenv("REMINDER_QUEUE_EMAIL_CONCURRENCY", "0")

	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestRunReturnsFailureForInvalidReminderQueueSmsConcurrency(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://worker@localhost:1/none")
	t.Setenv("APP_ENV", "development")
	t.Setenv("REMINDER_QUEUE_SMS_CONCURRENCY", "0")

	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestRunReturnsFailureForInvalidReminderJobMaxAttempts(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://worker@localhost:1/none")
	t.Setenv("APP_ENV", "development")
	t.Setenv("REMINDER_JOB_MAX_ATTEMPTS", "0")

	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestRunReturnsFailureForUnparsableReminderJobMaxAttempts(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://worker@localhost:1/none")
	t.Setenv("APP_ENV", "development")
	t.Setenv("REMINDER_JOB_MAX_ATTEMPTS", "not-a-number")

	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestReminderQueuesDisabledSms(t *testing.T) {
	queues := reminderQueues(config.Config{
		ReminderSmsAdapter:            config.ReminderSmsAdapterDisabled,
		ReminderQueueEmailConcurrency: 2,
		ReminderQueueSmsConcurrency:   3,
	})
	if _, ok := queues["reminder_sms"]; ok {
		t.Fatalf("reminder_sms queue present while the sms adapter is disabled")
	}
	email, ok := queues["reminder_email"]
	if !ok || email.MaxWorkers != 2 {
		t.Fatalf("reminder_email queue = %#v", queues)
	}
}

func TestReminderQueuesDefaultSms(t *testing.T) {
	queues := reminderQueues(config.Config{
		ReminderSmsAdapter:            config.ReminderSmsAdapterFake,
		ReminderQueueEmailConcurrency: 2,
		ReminderQueueSmsConcurrency:   3,
	})
	sms, ok := queues["reminder_sms"]
	if !ok || sms.MaxWorkers != 3 {
		t.Fatalf("reminder_sms queue = %#v", queues)
	}
}
