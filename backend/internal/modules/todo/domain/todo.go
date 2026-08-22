// Package domain holds the Todo context's aggregate and invariants.
package domain

import (
	"time"
	"unicode/utf8"
)

// Todo is the aggregate root for a user's committed task.
type Todo struct {
	ID              string
	WorkspaceID     string
	OwnerUserID     string
	Title           string
	Description     *string
	DueAtUTC        *time.Time
	TimezoneAtInput *string
	Status          Status
	ReminderVersion int
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
	DeletedAt       *time.Time
}

// New builds a pending todo. The due instant is optional; todos without a
// due never carry reminder plans.
func New(id, workspaceID, ownerUserID, title string, description *string, dueAtUTC *time.Time, timezoneAtInput *string, now time.Time) (Todo, error) {
	if err := validateTitle(title); err != nil {
		return Todo{}, err
	}
	return Todo{
		ID:              id,
		WorkspaceID:     workspaceID,
		OwnerUserID:     ownerUserID,
		Title:           title,
		Description:     description,
		DueAtUTC:        dueAtUTC,
		TimezoneAtInput: timezoneAtInput,
		Status:          StatusPending,
		ReminderVersion: 1,
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

// Restore rebuilds a todo in any status with its historical identity and
// timestamps; it validates the title and status only — reminder planning is
// the caller's concern and import never plans.
func Restore(id, workspaceID, ownerUserID, title string, description *string,
	dueAtUTC *time.Time, timezoneAtInput *string, status Status,
	reminderVersion, version int, createdAt, updatedAt time.Time,
	completedAt, deletedAt *time.Time) (Todo, error) {
	if id == "" || workspaceID == "" || ownerUserID == "" {
		return Todo{}, ErrMissingRestoreFields
	}
	if err := validateTitle(title); err != nil {
		return Todo{}, err
	}
	switch status {
	case StatusPending, StatusCompleted, StatusDeleted:
	default:
		return Todo{}, ErrInvalidStatus
	}
	if status == StatusCompleted && completedAt == nil {
		return Todo{}, ErrInconsistentStatus
	}
	if status == StatusDeleted && deletedAt == nil {
		return Todo{}, ErrInconsistentStatus
	}
	return Todo{
		ID:              id,
		WorkspaceID:     workspaceID,
		OwnerUserID:     ownerUserID,
		Title:           title,
		Description:     description,
		DueAtUTC:        dueAtUTC,
		TimezoneAtInput: timezoneAtInput,
		Status:          status,
		ReminderVersion: reminderVersion,
		Version:         version,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		CompletedAt:     completedAt,
		DeletedAt:       deletedAt,
	}, nil
}

func validateTitle(title string) error {
	if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > MaxTitleLength {
		return ErrInvalidTitle
	}
	return nil
}

// IsPending reports whether the todo awaits work.
func (t *Todo) IsPending() bool { return t.Status == StatusPending }

// IsOverdue derives overdue from state; it is never stored.
func (t *Todo) IsOverdue(now time.Time) bool {
	return t.IsPending() && t.DueAtUTC != nil && t.DueAtUTC.Before(now)
}

// Complete transitions a pending todo to completed.
func (t *Todo) Complete(version int, now time.Time) error {
	if t.Status == StatusDeleted {
		return ErrTodoDeleted
	}
	if t.Status == StatusCompleted {
		return ErrAlreadyCompleted
	}
	if version != t.Version {
		return ErrConflict
	}
	completedAt := now
	t.Status = StatusCompleted
	t.CompletedAt = &completedAt
	t.Version++
	return nil
}

// Delete soft-deletes a todo; the state is terminal.
func (t *Todo) Delete(version int, now time.Time) error {
	if t.Status == StatusDeleted {
		return ErrTodoDeleted
	}
	if version != t.Version {
		return ErrConflict
	}
	deletedAt := now
	t.Status = StatusDeleted
	t.DeletedAt = &deletedAt
	t.Version++
	return nil
}

// UpdateChanges carries partial edit fields; nil pointers leave the field
// untouched. DueChanged distinguishes "field absent" from "due cleared".
type UpdateChanges struct {
	Title           *string
	Description     *string
	TimezoneAtInput *string
	DueChanged      bool
	DueAtUTC        *time.Time
}

// Update applies an edit. Changing the due instant (including clearing it)
// bumps ReminderVersion so the reminder seam can revoke the old plan and
// schedule the new one.
func (t *Todo) Update(version int, changes UpdateChanges, now time.Time) error {
	if t.Status == StatusDeleted {
		return ErrTodoDeleted
	}
	if version != t.Version {
		return ErrConflict
	}
	if changes.Title != nil {
		if err := validateTitle(*changes.Title); err != nil {
			return err
		}
		t.Title = *changes.Title
	}
	if changes.Description != nil {
		description := *changes.Description
		t.Description = &description
	}
	if changes.TimezoneAtInput != nil {
		timezone := *changes.TimezoneAtInput
		t.TimezoneAtInput = &timezone
	}
	if changes.DueChanged && !sameTime(t.DueAtUTC, changes.DueAtUTC) {
		t.DueAtUTC = copyTime(changes.DueAtUTC)
		t.ReminderVersion++
	}
	t.Version++
	t.UpdatedAt = now
	return nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func copyTime(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}
