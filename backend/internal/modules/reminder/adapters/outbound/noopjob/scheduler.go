// Package noopjob provides the ITER-0002 JobScheduler adapter: it accepts
// every job and schedules nothing. ITER-0003 replaces it with River.
package noopjob

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

type scheduler struct{}

// New returns the no-op JobScheduler.
func New() ports.JobScheduler { return scheduler{} }

// Schedule accepts the job and does nothing.
func (scheduler) Schedule(context.Context, ports.ReminderJob) error { return nil }
