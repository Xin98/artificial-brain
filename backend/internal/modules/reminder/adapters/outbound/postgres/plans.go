// Package postgres implements the reminder PlanStore on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

const uniqueViolation = "23505"

// PlanStore persists reminder plans in PostgreSQL.
type PlanStore struct {
	pool *pgxpool.Pool
}

// NewPlanStore returns a PlanStore bound to pool.
func NewPlanStore(pool *pgxpool.Pool) *PlanStore { return &PlanStore{pool: pool} }

// Save inserts a plan, mapping the unique (todo_id, todo_reminder_version)
// constraint to domain.ErrPlanExists so callers can treat replanning as
// idempotent.
func (s *PlanStore) Save(ctx context.Context, plan domain.ReminderPlan) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into reminder.reminder_plans
			(id, workspace_id, todo_id, todo_reminder_version, scheduled_at_utc,
			 requested_channels, status, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
	`, plan.ID, plan.WorkspaceID, plan.TodoID, plan.TodoReminderVersion,
		plan.ScheduledAtUTC, plan.RequestedChannels, string(plan.Status), plan.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrPlanExists
	}
	return err
}

// RevokePlanned conditionally revokes planned plans at or below the version
// cutoff; already-revoked rows and versions above the cutoff are untouched.
func (s *PlanStore) RevokePlanned(ctx context.Context, workspaceID, todoID string, upToReminderVersion int, now time.Time) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		update reminder.reminder_plans
		set status = 'revoked', revoked_at = $4
		where workspace_id = $1 and todo_id = $2
		  and todo_reminder_version <= $3 and status = 'planned'
	`, workspaceID, todoID, upToReminderVersion, now)
	return err
}
