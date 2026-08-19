package ports

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// PlanStore persists reminder plans. Implementations resolve their executor
// from context so writes join the caller's ambient transaction.
type PlanStore interface {
	// Save inserts a plan; a duplicate (todo_id, todo_reminder_version)
	// returns domain.ErrPlanExists.
	Save(ctx context.Context, plan domain.ReminderPlan) error
	// Get loads one plan scoped by workspace; a missing row or a plan in
	// another workspace returns domain.ErrPlanNotFound.
	Get(ctx context.Context, workspaceID, planID string) (domain.ReminderPlan, error)
	// RevokePlanned revokes every planned plan for the todo whose
	// todo_reminder_version is at most upToReminderVersion.
	RevokePlanned(ctx context.Context, workspaceID, todoID string, upToReminderVersion int, now time.Time) error
}
