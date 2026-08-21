package domain

import (
	"strconv"
	"time"
)

// DeliveryState is the lifecycle state of a reminder delivery.
type DeliveryState string

const (
	// StateScheduled marks a delivery awaiting its scheduled instant.
	StateScheduled DeliveryState = "scheduled"
	// StateSending marks a delivery a worker is submitting to the provider;
	// retries stay in this state with a growing AttemptCount.
	StateSending DeliveryState = "sending"
	// StateSucceeded marks a delivery the provider accepted. Terminal.
	StateSucceeded DeliveryState = "succeeded"
	// StateFailed marks a permanently failed delivery, a business dead
	// letter. Terminal.
	StateFailed DeliveryState = "failed"
	// StateSuppressed marks a delivery cancelled by an execution-time
	// re-read. Terminal, always carrying a SuppressionReason.
	StateSuppressed DeliveryState = "suppressed"
)

// SuppressionReason records why an execution-time re-read cancelled a
// delivery.
type SuppressionReason string

const (
	// ReasonTodoCompleted suppresses because the todo is already completed.
	ReasonTodoCompleted SuppressionReason = "todo_completed"
	// ReasonTodoDeleted suppresses because the todo was deleted.
	ReasonTodoDeleted SuppressionReason = "todo_deleted"
	// ReasonVersionStale suppresses because the todo was rescheduled to a
	// newer reminder version.
	ReasonVersionStale SuppressionReason = "version_stale"
	// ReasonChannelUnavailable suppresses because the channel is missing,
	// unverified, or disabled at execution time.
	ReasonChannelUnavailable SuppressionReason = "channel_unavailable"
	// ReasonPlanRevoked suppresses because the owning plan was revoked.
	ReasonPlanRevoked SuppressionReason = "plan_revoked"
)

// NewSuppressionReason parses a raw suppression reason carried across the
// module seam, accepting only the five known reasons; anything else returns
// ErrInvalidSuppressionReason so free-form text can never reach the store.
func NewSuppressionReason(value string) (SuppressionReason, error) {
	switch reason := SuppressionReason(value); reason {
	case ReasonTodoCompleted, ReasonTodoDeleted, ReasonVersionStale, ReasonChannelUnavailable, ReasonPlanRevoked:
		return reason, nil
	}
	return "", ErrInvalidSuppressionReason
}

// ReceiptState is the provider delivery-receipt verdict. Receipts are
// informational and never change the delivery's terminal state.
type ReceiptState string

const (
	// ReceiptOK records the provider reporting successful delivery.
	ReceiptOK ReceiptState = "received_ok"
	// ReceiptFailed records the provider reporting failed delivery.
	ReceiptFailed ReceiptState = "received_failed"
)

// DeliveryOrigin distinguishes plan-time deliveries from imported history.
// The zero value is the local origin, so every existing construction site
// stays untouched; the database default mirrors it with 'local'.
type DeliveryOrigin string

const (
	// OriginLocal marks a delivery planned and executed by this instance.
	OriginLocal DeliveryOrigin = "local"
	// OriginImported marks a delivery restored from another instance's
	// export bundle.
	OriginImported DeliveryOrigin = "imported"
)

// ReminderDelivery tracks one attempt to deliver a planned reminder over one
// channel. One delivery is created per requested channel, atomically with the
// plan; the UNIQUE IdempotencyKey keeps replanning and retries on a single
// row. Terminal states (succeeded, failed, suppressed) are immutable.
type ReminderDelivery struct {
	ID                  string
	WorkspaceID         string
	TodoID              string
	OwnerUserID         string // denormalized from the todo at plan time; keeps every execution-time read workspace+user scoped
	TodoReminderVersion int
	PlanID              string
	Channel             string
	TodoTitleSnapshot   string
	IdempotencyKey      string
	State               DeliveryState
	SuppressionReason   *SuppressionReason
	AttemptCount        int
	ProviderJobID       *int64
	ProviderMessageID   *string
	LastErrorCode       *string
	ScheduledAt         time.Time
	CreatedAt           time.Time
	SubmittedAt         *time.Time
	FinalizedAt         *time.Time
	ReceiptState        *ReceiptState
	ReceiptAt           *time.Time
	ReceiptErrorCode    *string
	Origin              DeliveryOrigin // zero value is the local origin; only RestoreDelivery stamps the imported origin
}

// IdempotencyKeyFor builds the business idempotency key
// "workspaceId:todoId:todoReminderVersion:channel" enforced by a UNIQUE
// constraint.
func IdempotencyKeyFor(workspaceID, todoID string, todoReminderVersion int, channel string) string {
	return workspaceID + ":" + todoID + ":" + strconv.Itoa(todoReminderVersion) + ":" + channel
}

// NewDelivery builds a scheduled delivery for one channel of one plan. The
// identity fields and title snapshot are required, the scheduled instant must
// be set, and the channel must be "email" or "sms".
func NewDelivery(id, workspaceID, ownerUserID, todoID string, todoReminderVersion int, planID, channel, titleSnapshot string, scheduledAt, now time.Time) (ReminderDelivery, error) {
	if id == "" || workspaceID == "" || ownerUserID == "" || todoID == "" || planID == "" || titleSnapshot == "" {
		return ReminderDelivery{}, ErrMissingDeliveryFields
	}
	if scheduledAt.IsZero() {
		return ReminderDelivery{}, ErrMissingSchedule
	}
	if channel != "email" && channel != "sms" {
		return ReminderDelivery{}, ErrInvalidDeliveryChannel
	}
	return ReminderDelivery{
		ID:                  id,
		WorkspaceID:         workspaceID,
		TodoID:              todoID,
		OwnerUserID:         ownerUserID,
		TodoReminderVersion: todoReminderVersion,
		PlanID:              planID,
		Channel:             channel,
		TodoTitleSnapshot:   titleSnapshot,
		IdempotencyKey:      IdempotencyKeyFor(workspaceID, todoID, todoReminderVersion, channel),
		State:               StateScheduled,
		ScheduledAt:         scheduledAt,
		CreatedAt:           now,
	}, nil
}

// RestoreDelivery rebuilds a historical delivery without a plan; the
// idempotency key is the caller's import key, states must be terminal or
// scheduled-with-history (all five states allowed — history is history). The
// restored delivery carries the imported origin; imported history is
// read-only and never transitions again.
func RestoreDelivery(id, workspaceID, ownerUserID, todoID string, todoReminderVersion int,
	channel, titleSnapshot, idempotencyKey string, state DeliveryState,
	suppressionReason *SuppressionReason, attemptCount int,
	providerMessageID, lastErrorCode *string,
	scheduledAt, createdAt time.Time, submittedAt, finalizedAt *time.Time,
	receiptState *ReceiptState, receiptAt *time.Time, receiptErrorCode *string) (ReminderDelivery, error) {
	if id == "" || workspaceID == "" || ownerUserID == "" || todoID == "" || titleSnapshot == "" || idempotencyKey == "" {
		return ReminderDelivery{}, ErrMissingDeliveryFields
	}
	if channel != "email" && channel != "sms" {
		return ReminderDelivery{}, ErrInvalidDeliveryChannel
	}
	if attemptCount < 0 {
		return ReminderDelivery{}, ErrInvalidDeliveryAttemptCount
	}
	return ReminderDelivery{
		ID:                  id,
		WorkspaceID:         workspaceID,
		TodoID:              todoID,
		OwnerUserID:         ownerUserID,
		TodoReminderVersion: todoReminderVersion,
		Channel:             channel,
		TodoTitleSnapshot:   titleSnapshot,
		IdempotencyKey:      idempotencyKey,
		State:               state,
		SuppressionReason:   suppressionReason,
		AttemptCount:        attemptCount,
		ProviderMessageID:   providerMessageID,
		LastErrorCode:       lastErrorCode,
		ScheduledAt:         scheduledAt,
		CreatedAt:           createdAt,
		SubmittedAt:         submittedAt,
		FinalizedAt:         finalizedAt,
		ReceiptState:        receiptState,
		ReceiptAt:           receiptAt,
		ReceiptErrorCode:    receiptErrorCode,
		Origin:              OriginImported,
	}, nil
}

// IsFinal reports whether the delivery reached a terminal state; final
// deliveries never transition again.
func (d *ReminderDelivery) IsFinal() bool {
	switch d.State {
	case StateSucceeded, StateFailed, StateSuppressed:
		return true
	}
	return false
}

// MarkSending records a worker picking the delivery up, incrementing the
// attempt count. Allowed from scheduled and sending; now is taken for clock
// symmetry with the other transitions.
func (d *ReminderDelivery) MarkSending(now time.Time) error {
	if d.IsFinal() {
		return ErrDeliveryFinal
	}
	d.State = StateSending
	d.AttemptCount++
	return nil
}

// MarkSucceeded records the provider accepting the delivery, finalizing it at
// the submission instant. Allowed from sending only.
func (d *ReminderDelivery) MarkSucceeded(providerMessageID string, now time.Time) error {
	if d.IsFinal() {
		return ErrDeliveryFinal
	}
	if d.State != StateSending {
		return ErrDeliveryNotSending
	}
	d.State = StateSucceeded
	d.ProviderMessageID = &providerMessageID
	d.SubmittedAt = &now
	d.FinalizedAt = &now
	return nil
}

// MarkFailed records a permanent provider failure or exhausted retries,
// finalizing the delivery as a business dead letter. Allowed from scheduled
// and sending.
func (d *ReminderDelivery) MarkFailed(errorCode string, now time.Time) error {
	if d.IsFinal() {
		return ErrDeliveryFinal
	}
	d.State = StateFailed
	d.LastErrorCode = &errorCode
	d.FinalizedAt = &now
	return nil
}

// MarkSuppressed records an execution-time re-read cancelling the delivery,
// finalizing it with the given reason. Allowed from scheduled and sending.
func (d *ReminderDelivery) MarkSuppressed(reason SuppressionReason, now time.Time) error {
	if d.IsFinal() {
		return ErrDeliveryFinal
	}
	d.State = StateSuppressed
	d.SuppressionReason = &reason
	d.FinalizedAt = &now
	return nil
}

// ApplyReceipt records a provider delivery receipt. Only succeeded deliveries
// carry receipts; the first receipt wins and later receipts are idempotent
// no-ops. The delivery's terminal state is never changed.
func (d *ReminderDelivery) ApplyReceipt(state ReceiptState, errorCode string, now time.Time) error {
	if d.State != StateSucceeded {
		return ErrReceiptNotApplicable
	}
	if d.ReceiptState != nil {
		return nil
	}
	d.ReceiptState = &state
	d.ReceiptAt = &now
	if errorCode != "" {
		d.ReceiptErrorCode = &errorCode
	}
	return nil
}
