package ports

import (
	"context"
	"time"
)

// ReminderJob describes the scheduled work a delivery backend (River in
// ITER-0003) must perform for one reminder plan. OwnerUserID travels with the
// job so every execution-time read stays workspace+user scoped.
type ReminderJob struct {
	PlanID              string
	WorkspaceID         string
	TodoID              string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	Channels            []string
	OwnerUserID         string
}

// ScheduledChannel pairs one requested channel with the provider job ID the
// scheduler assigned to it, so the planner can write the ID back onto the
// matching delivery row.
type ScheduledChannel struct {
	Channel string
	JobID   int64
}

// JobScheduler enqueues reminder jobs. ITER-0002 ships only the no-op
// adapter; ITER-0003 swaps in River. Schedule fans out one provider job per
// channel and returns their IDs; Cancel is best-effort and its errors are the
// caller's policy.
type JobScheduler interface {
	Schedule(ctx context.Context, job ReminderJob) ([]ScheduledChannel, error)
	Cancel(ctx context.Context, jobID int64) error
}
