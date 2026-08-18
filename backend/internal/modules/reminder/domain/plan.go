// Package domain holds the Reminder context's aggregates and invariants.
// ITER-0002 only introduces the Reminder Plan seam: plans are written in the
// same transaction as Todo changes and nothing is delivered yet.
package domain

import "time"

// PlanStatus is the lifecycle state of a reminder plan.
type PlanStatus string

const (
	// StatusPlanned marks a plan awaiting its scheduled instant.
	StatusPlanned PlanStatus = "planned"
	// StatusRevoked marks a plan cancelled by a todo transition.
	StatusRevoked PlanStatus = "revoked"
)

// ReminderPlan records that a todo revision should trigger a reminder at a
// scheduled instant. The pair (TodoID, TodoReminderVersion) is unique so a
// rescheduled todo never carries two live plans for the same revision.
type ReminderPlan struct {
	ID                  string
	WorkspaceID         string
	TodoID              string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	RequestedChannels   []string
	Status              PlanStatus
	CreatedAt           time.Time
	RevokedAt           *time.Time
}

// NewReminderPlan builds a planned reminder. ScheduledAtUTC is required;
// channels may be empty but are normalized to a non-nil slice.
func NewReminderPlan(id, workspaceID, todoID string, todoReminderVersion int, scheduledAtUTC time.Time, channels []string, now time.Time) (ReminderPlan, error) {
	if scheduledAtUTC.IsZero() {
		return ReminderPlan{}, ErrMissingSchedule
	}
	normalized := make([]string, len(channels))
	copy(normalized, channels)
	return ReminderPlan{
		ID:                  id,
		WorkspaceID:         workspaceID,
		TodoID:              todoID,
		TodoReminderVersion: todoReminderVersion,
		ScheduledAtUTC:      scheduledAtUTC,
		RequestedChannels:   normalized,
		Status:              StatusPlanned,
		CreatedAt:           now,
	}, nil
}

// IsRevoked reports whether the plan has been revoked.
func (p *ReminderPlan) IsRevoked() bool { return p.Status == StatusRevoked }

// Revoke transitions a planned plan to revoked exactly once.
func (p *ReminderPlan) Revoke(now time.Time) error {
	if p.IsRevoked() {
		return ErrPlanAlreadyRevoked
	}
	p.Status = StatusRevoked
	p.RevokedAt = &now
	return nil
}
