package ports

import (
	"context"
	"time"
)

// PlanReminderRequest is Todo's consumer-owned request to schedule a
// reminder for one todo reminder version. Title and OwnerUserID snapshot
// the todo's title and owner at plan time so deliveries can address the
// owner without joining back to the todo module.
type PlanReminderRequest struct {
	WorkspaceID         string
	TodoID              string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	Channels            []string
	Title               string
	OwnerUserID         string
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

// ReminderCounts buckets a workspace's reminder deliveries by terminal and
// in-flight lifecycle state, mirrored from the Reminder context's delivery
// counts without importing it.
type ReminderCounts struct {
	Succeeded  int
	Retrying   int
	Failed     int
	Suppressed int
}

// ReminderStats reports a workspace's reminder delivery counts. It is the
// seam to the Reminder context, adapted in cmd; a nil ReminderStats means
// reminders are not wired and all counters stay zero.
type ReminderStats func(ctx context.Context, workspaceID string) (ReminderCounts, error)
