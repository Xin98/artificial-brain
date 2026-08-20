// Package noopjob provides the ITER-0002 JobScheduler adapter: it accepts
// every job and schedules nothing. ITER-0003 retires it from the cmd/api
// wiring in favor of River and keeps it only as a test fake.
package noopjob

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

type scheduler struct{}

// New returns the no-op JobScheduler.
func New() ports.JobScheduler { return scheduler{} }

// Schedule accepts the job, schedules nothing, and returns an empty slice.
func (scheduler) Schedule(context.Context, ports.ReminderJob) ([]ports.ScheduledChannel, error) {
	return []ports.ScheduledChannel{}, nil
}

// Cancel accepts the cancel and does nothing.
func (scheduler) Cancel(context.Context, int64) error { return nil }
