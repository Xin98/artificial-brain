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
// counters come from ReminderStats and stay zero when it is nil (reminders
// not wired).
type DashboardSummaryHandler struct {
	Store         ports.TodoStore
	Now           func() time.Time
	ReminderStats ports.ReminderStats
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
	counts, err := h.reminderCounts(ctx, workspaceID)
	if err != nil {
		return dto.DashboardSummary{}, err
	}
	summary.ReminderSucceeded = counts.Succeeded
	summary.ReminderRetrying = counts.Retrying
	summary.ReminderFailed = counts.Failed
	summary.ReminderSuppressed = counts.Suppressed
	summary.CheckedAt = now
	return summary, nil
}

// reminderCounts resolves the workspace's reminder delivery counts, treating
// a nil ReminderStats as all-zero counts (reminders not wired).
func (h *DashboardSummaryHandler) reminderCounts(ctx context.Context, workspaceID string) (ports.ReminderCounts, error) {
	if h.ReminderStats == nil {
		return ports.ReminderCounts{}, nil
	}
	return h.ReminderStats(ctx, workspaceID)
}
