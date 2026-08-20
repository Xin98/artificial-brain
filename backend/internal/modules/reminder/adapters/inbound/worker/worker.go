// Package worker is the ITER-0003 inbound adapter: it turns River jobs into
// SendReminder application-command calls, mapping the queue's attempt counter
// onto the command's final-attempt dead-letter policy and supplying the
// iteration's capped exponential retry backoff.
package worker

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
)

// retryBaseDelay is the first retry delay; each subsequent attempt doubles it.
const retryBaseDelay = 500 * time.Millisecond

// retryMaxDelay caps the exponential backoff.
const retryMaxDelay = 60 * time.Second

// SendWorker executes reminder_send jobs by delegating to the application's
// SendReminderHandler. MaxAttempts mirrors the queue's per-job attempt limit
// so the worker can mark the last attempt: a transient failure on it
// dead-letters the delivery instead of bouncing back to the queue.
type SendWorker struct {
	river.WorkerDefaults[dto.ReminderSendArgs]
	Handler     *command.SendReminderHandler
	MaxAttempts int
}

var _ river.Worker[dto.ReminderSendArgs] = (*SendWorker)(nil)

// Work delivers one channel of one planned reminder.
func (w *SendWorker) Work(ctx context.Context, job *river.Job[dto.ReminderSendArgs]) error {
	return w.Handler.Handle(ctx, command.SendRequest{
		WorkspaceID:  job.Args.WorkspaceID,
		OwnerUserID:  job.Args.OwnerUserID,
		PlanID:       job.Args.PlanID,
		Channel:      job.Args.Channel,
		FinalAttempt: job.Attempt >= w.MaxAttempts,
	})
}

// NextRetryDelay returns the capped exponential backoff before the next
// attempt: 500ms after the first failure, doubling per attempt, capped at 60s.
func (w *SendWorker) NextRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// 500ms * 2^7 already exceeds the cap, and returning early also keeps the
	// shift from ever growing past time.Duration's range.
	if attempt-1 >= 7 {
		return retryMaxDelay
	}
	return retryBaseDelay << uint(attempt-1)
}

// NextRetry wires the backoff into River: a failed job is retried
// NextRetryDelay(attempt) after the attempt that just failed.
func (w *SendWorker) NextRetry(job *river.Job[dto.ReminderSendArgs]) time.Time {
	return time.Now().Add(w.NextRetryDelay(job.Attempt))
}
