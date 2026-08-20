package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

// opsWindow bounds the submission-latency percentile to the trailing 24
// hours. It is a constant so the ops endpoint stays deterministic.
const opsWindow = 24 * time.Hour

// ReminderOpsHandler produces the instance-wide reminder operations snapshot.
type ReminderOpsHandler struct {
	Ops ports.OpsStore
	Now func() time.Time
}

// Handle returns the ops view computed over the trailing 24h window ending at
// the injected clock.
func (h *ReminderOpsHandler) Handle(ctx context.Context) (dto.OpsView, error) {
	return h.Ops.ReminderOps(ctx, h.Now(), opsWindow)
}
