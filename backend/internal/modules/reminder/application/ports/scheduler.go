package ports

import (
	"context"
	"time"
)

// ReminderJob describes the scheduled work a future delivery backend (River
// in ITER-0003) must perform for one reminder plan.
type ReminderJob struct {
	PlanID              string
	WorkspaceID         string
	TodoID              string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	Channels            []string
}

// JobScheduler enqueues reminder jobs. ITER-0002 ships only the no-op
// adapter; the port exists so ITER-0003 swaps in River without touching the
// application layer.
type JobScheduler interface {
	Schedule(ctx context.Context, job ReminderJob) error
}
