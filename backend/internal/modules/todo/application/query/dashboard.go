package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// DashboardSummaryHandler computes the deterministic dashboard. The local
// "today" window travels per request as an IANA timezone; the reminder
// counters stay zero until ITER-0003 (D7).
type DashboardSummaryHandler struct {
	Store ports.TodoStore
	Now   func() time.Time
}

// Handle returns the summary for the caller's workspace.
func (h *DashboardSummaryHandler) Handle(ctx context.Context, workspaceID, ownerUserID, timezone string) (dto.DashboardSummary, error) {
	if timezone == "" {
		return dto.DashboardSummary{}, domain.ErrInvalidTimezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil || location == nil {
		return dto.DashboardSummary{}, domain.ErrInvalidTimezone
	}
	now := h.Now()
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1)

	summary, err := h.Store.Dashboard(ctx, workspaceID, ownerUserID, now, dayStart, dayEnd)
	if err != nil {
		return dto.DashboardSummary{}, err
	}
	summary.ReminderRetrying = 0
	summary.ReminderFailed = 0
	summary.CheckedAt = now
	return summary, nil
}
