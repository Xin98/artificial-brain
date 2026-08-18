package domain

import "time"

// ConfirmationRequest is a short-lived, single-use confirmation bound to the
// requesting user, workspace, todo, and todo version. Both smart and manual
// deletes flow through it.
type ConfirmationRequest struct {
	ID          string
	WorkspaceID string
	UserID      string
	Intent      Intent
	TodoID      string
	TodoVersion int
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
}

// NewConfirmationRequest builds an unconsumed confirmation. Only destructive
// intents are confirmable.
func NewConfirmationRequest(id, workspaceID, userID string, intent Intent, todoID string, todoVersion int, now time.Time, ttl time.Duration) (ConfirmationRequest, error) {
	if intent != IntentTodoDelete {
		return ConfirmationRequest{}, ErrUnsupportedConfirmationIntent
	}
	return ConfirmationRequest{
		ID:          id,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Intent:      intent,
		TodoID:      todoID,
		TodoVersion: todoVersion,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}, nil
}

// IsConsumed reports whether the confirmation was already used.
func (c *ConfirmationRequest) IsConsumed() bool { return c.ConsumedAt != nil }

// IsExpired reports whether now has reached or passed the deadline.
func (c *ConfirmationRequest) IsExpired(now time.Time) bool {
	return !now.Before(c.ExpiresAt)
}

// Consume marks the confirmation used exactly once within its TTL window.
func (c *ConfirmationRequest) Consume(now time.Time) error {
	if c.IsConsumed() {
		return ErrConfirmationConsumed
	}
	if c.IsExpired(now) {
		return ErrConfirmationExpired
	}
	consumedAt := now
	c.ConsumedAt = &consumedAt
	return nil
}
