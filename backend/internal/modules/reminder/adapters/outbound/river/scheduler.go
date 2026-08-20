// Package river is the ITER-0003 JobScheduler adapter: it inserts one River
// job per requested channel into the caller's ambient transaction, so reminder
// jobs commit or roll back atomically with the plan and delivery rows they
// belong to.
package river

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	riverqueue "github.com/riverqueue/river"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// ErrNoAmbientTransaction reports that Schedule was called without an ambient
// database transaction. River jobs must commit atomically with the plan and
// delivery rows they deliver, so there is no non-transactional fallback.
var ErrNoAmbientTransaction = errors.New("reminderriver: Schedule requires an ambient database transaction")

// ErrExecutorNotTransaction reports that the ambient executor is not a pgx
// transaction; River's transactional insert needs the concrete tx.
var ErrExecutorNotTransaction = errors.New("reminderriver: ambient executor is not a pgx transaction")

// Scheduler inserts reminder jobs into River, one per requested channel,
// inside the caller's ambient transaction.
type Scheduler struct {
	client *riverqueue.Client[pgx.Tx]
}

// New returns a Scheduler over client. The client only needs its driver's
// pool for inserts; it never has to be started.
func New(client *riverqueue.Client[pgx.Tx]) *Scheduler { return &Scheduler{client: client} }

var _ ports.JobScheduler = (*Scheduler)(nil)

// Schedule inserts one job per channel on the reminder_<channel> queue,
// scheduled at the plan's instant, joining the ambient transaction. Both
// failure modes — a missing ambient transaction or an executor that is not a
// pgx.Tx — return typed errors instead of silently falling back.
func (s *Scheduler) Schedule(ctx context.Context, job ports.ReminderJob) ([]ports.ScheduledChannel, error) {
	executor, ok := database.ExecutorFromContext(ctx)
	if !ok {
		return nil, ErrNoAmbientTransaction
	}
	tx, ok := executor.(pgx.Tx)
	if !ok {
		return nil, ErrExecutorNotTransaction
	}

	scheduled := make([]ports.ScheduledChannel, 0, len(job.Channels))
	for _, channel := range job.Channels {
		result, err := s.client.InsertTx(ctx, tx, dto.ReminderSendArgs{
			PlanID:              job.PlanID,
			WorkspaceID:         job.WorkspaceID,
			OwnerUserID:         job.OwnerUserID,
			TodoID:              job.TodoID,
			TodoReminderVersion: job.TodoReminderVersion,
			Channel:             channel,
		}, &riverqueue.InsertOpts{
			Queue:       "reminder_" + channel,
			ScheduledAt: job.ScheduledAtUTC,
		})
		if err != nil {
			return nil, err
		}
		scheduled = append(scheduled, ports.ScheduledChannel{Channel: channel, JobID: result.Job.ID})
	}
	return scheduled, nil
}

// Cancel cancels one queued job; errors are passed through because
// best-effort is the caller's policy.
func (s *Scheduler) Cancel(ctx context.Context, jobID int64) error {
	_, err := s.client.JobCancel(ctx, jobID)
	return err
}
