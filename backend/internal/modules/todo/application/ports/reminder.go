package ports

import (
	"context"
	"time"
)

// PlanReminderRequest is Todo's consumer-owned request to schedule a
// reminder for one todo reminder version.
type PlanReminderRequest struct {
	WorkspaceID         string
	TodoID              string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	Channels            []string
}

// RevokeReminderRequest is Todo's consumer-owned request to revoke planned
// reminders up to a reminder version cutoff.
type RevokeReminderRequest struct {
	WorkspaceID         string
	TodoID              string
	UpToReminderVersion int
}

// ReminderPlanner is the seam to the Reminder context, satisfied in cmd by
// Reminder's public application handlers.
type ReminderPlanner interface {
	Plan(ctx context.Context, request PlanReminderRequest) error
	Revoke(ctx context.Context, request RevokeReminderRequest) error
}

// ChannelsProvider snapshots the owner's reminder channel kinds at plan
// time. It may be nil, in which case plans carry an empty snapshot.
type ChannelsProvider func(ctx context.Context, workspaceID, ownerUserID string) ([]string, error)
