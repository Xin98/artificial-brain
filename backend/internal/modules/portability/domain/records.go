package domain

import (
	"fmt"
	"time"
)

// Todo record statuses carried by bundles; they mirror the todo module's
// Status values without importing another context's domain.
const (
	TodoStatusPending   = "pending"
	TodoStatusCompleted = "completed"
	TodoStatusDeleted   = "deleted"
)

// Channel kinds carried by bundles.
const (
	ChannelKindEmail = "email"
	ChannelKindSMS   = "sms"
)

// Delivery states carried by bundles; they mirror the reminder module's
// DeliveryState values.
const (
	DeliveryStateScheduled  = "scheduled"
	DeliveryStateSending    = "sending"
	DeliveryStateSucceeded  = "succeeded"
	DeliveryStateFailed     = "failed"
	DeliveryStateSuppressed = "suppressed"
)

// Delivery origins carried by bundles; they mirror the migration 008 check
// constraint on reminder.reminder_deliveries.origin.
const (
	DeliveryOriginLocal    = "local"
	DeliveryOriginImported = "imported"
)

// TodoRecord is a todo row as carried by an export bundle.
type TodoRecord struct {
	ID              string
	Title           string
	Description     *string
	DueAtUTC        *time.Time
	TimezoneAtInput *string
	Status          string // pending|completed|deleted
	ReminderVersion int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
	DeletedAt       *time.Time
}

// ChannelRecord is a contact channel row as carried by an export bundle.
type ChannelRecord struct {
	ID      string
	Kind    string
	Address string
	Enabled bool
}

// DeliveryRecord is a reminder delivery row as carried by an export bundle.
type DeliveryRecord struct {
	ID                 string
	SourceTodoRecordID string
	Channel            string
	State              string
	SuppressionReason  *string
	ProviderMessageID  *string
	LastErrorCode      *string
	AttemptCount       int
	TodoTitleSnapshot  string
	ScheduledAt        time.Time
	CreatedAt          time.Time
	SubmittedAt        *time.Time
	FinalizedAt        *time.Time
	ReceiptState       *string
	ReceiptErrorCode   *string
	ReceiptAt          *time.Time
	Origin             string
}

// ValidateTodoRecord checks a bundle todo record; violations report
// ErrRecordInvalid with a field reason.
func ValidateTodoRecord(r TodoRecord) error {
	if r.ID == "" {
		return recordError(r.ID, "todo: id is required")
	}
	if r.Title == "" {
		return recordError(r.ID, "todo: title is required")
	}
	switch r.Status {
	case TodoStatusPending, TodoStatusCompleted, TodoStatusDeleted:
	default:
		return recordError(r.ID, fmt.Sprintf("todo: unknown status %q", r.Status))
	}
	return nil
}

// ValidateChannelRecord checks a bundle channel record; the kind must be a
// known channel kind and the address non-empty.
func ValidateChannelRecord(r ChannelRecord) error {
	if r.ID == "" {
		return recordError(r.ID, "channel: id is required")
	}
	switch r.Kind {
	case ChannelKindEmail, ChannelKindSMS:
	default:
		return recordError(r.ID, fmt.Sprintf("channel: unknown kind %q", r.Kind))
	}
	if r.Address == "" {
		return recordError(r.ID, "channel: address is required")
	}
	return nil
}

// ValidateDeliveryRecord checks a bundle delivery record; violations report
// ErrRecordInvalid with a field reason.
func ValidateDeliveryRecord(r DeliveryRecord) error {
	if r.ID == "" {
		return recordError(r.ID, "delivery: id is required")
	}
	if r.SourceTodoRecordID == "" {
		return recordError(r.ID, "delivery: source todo record id is required")
	}
	switch r.Channel {
	case ChannelKindEmail, ChannelKindSMS:
	default:
		return recordError(r.ID, fmt.Sprintf("delivery: unknown channel %q", r.Channel))
	}
	switch r.State {
	case DeliveryStateScheduled, DeliveryStateSending, DeliveryStateSucceeded, DeliveryStateFailed, DeliveryStateSuppressed:
	default:
		return recordError(r.ID, fmt.Sprintf("delivery: unknown state %q", r.State))
	}
	if r.AttemptCount < 0 {
		return recordError(r.ID, "delivery: attempt count must not be negative")
	}
	switch r.Origin {
	case DeliveryOriginLocal, DeliveryOriginImported:
	default:
		return recordError(r.ID, fmt.Sprintf("delivery: unknown origin %q", r.Origin))
	}
	return nil
}

// recordError wraps ErrRecordInvalid with a field reason, naming the record
// when its id is known.
func recordError(recordID, reason string) error {
	if recordID == "" {
		return fmt.Errorf("%w: %s", ErrRecordInvalid, reason)
	}
	return fmt.Errorf("%w: record %s: %s", ErrRecordInvalid, recordID, reason)
}
