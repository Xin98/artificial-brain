package ports

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
)

// ErrPermanent marks a provider failure that must never be retried (invalid
// address, template rejected, provider explicitly refused). Adapters wrap it
// with context; the send command classifies with errors.Is. Any other error
// is transient and retried by the queue.
var ErrPermanent = errors.New("reminder: permanent provider failure")

// EmailNotifier submits one reminder over email.
type EmailNotifier interface {
	Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error)
}

// SmsNotifier submits one reminder over SMS.
type SmsNotifier interface {
	Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error)
}

// OpsStore answers the instance-wide reminder operations snapshot: queue
// depths, delivery lifecycle counts, retry rate, and the submission-latency
// percentile computed over the trailing window ending at now.
type OpsStore interface {
	ReminderOps(ctx context.Context, now time.Time, window time.Duration) (dto.OpsView, error)
}
