// Package postgres implements the todo TodoStore on PostgreSQL. Every query
// is scoped by workspace and owner; writes resolve their executor from
// context so they join the caller's ambient transaction.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// Store persists todos in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store bound to pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const todoColumns = `id, workspace_id, owner_user_id, title, description, due_at_utc,
	timezone_at_input, status, reminder_version, version,
	created_at, updated_at, completed_at, deleted_at`

// Insert persists a new todo.
func (s *Store) Insert(ctx context.Context, todo domain.Todo) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into todo.todos
			(id, workspace_id, owner_user_id, title, description, due_at_utc,
			 timezone_at_input, status, reminder_version, version,
			 created_at, updated_at, completed_at, deleted_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, todo.ID, todo.WorkspaceID, todo.OwnerUserID, todo.Title, todo.Description, todo.DueAtUTC,
		todo.TimezoneAtInput, string(todo.Status), todo.ReminderVersion, todo.Version,
		todo.CreatedAt, todo.UpdatedAt, todo.CompletedAt, todo.DeletedAt)
	return err
}

// Get loads one todo scoped to the caller.
func (s *Store) Get(ctx context.Context, workspaceID, ownerUserID, todoID string) (domain.Todo, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	todo, err := scanTodo(exec.QueryRow(ctx, `
		select `+todoColumns+`
		from todo.todos
		where id = $1 and workspace_id = $2 and owner_user_id = $3
	`, todoID, workspaceID, ownerUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Todo{}, domain.ErrTodoNotFound
	}
	return todo, err
}

// Update applies the todo when the stored row still carries expectedVersion;
// a stale or cross-workspace row yields domain.ErrConflict.
func (s *Store) Update(ctx context.Context, todo domain.Todo, expectedVersion int) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	tag, err := exec.Exec(ctx, `
		update todo.todos
		set title = $5, description = $6, due_at_utc = $7, timezone_at_input = $8,
		    status = $9, reminder_version = $10, version = $11, updated_at = $12,
		    completed_at = $13, deleted_at = $14
		where id = $1 and workspace_id = $2 and owner_user_id = $3 and version = $4
	`, todo.ID, todo.WorkspaceID, todo.OwnerUserID, expectedVersion,
		todo.Title, todo.Description, todo.DueAtUTC, todo.TimezoneAtInput,
		string(todo.Status), todo.ReminderVersion, todo.Version, todo.UpdatedAt,
		todo.CompletedAt, todo.DeletedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// List returns visible todos matching the combinable filters, capped at
// limit; deleted todos are never listed.
func (s *Store) List(ctx context.Context, workspaceID, ownerUserID string, filters dto.ListFilters, limit int) ([]domain.Todo, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	clauses := []string{"workspace_id = $1", "owner_user_id = $2", "status <> 'deleted'"}
	args := []any{workspaceID, ownerUserID}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if filters.Keyword != "" {
		add("title ilike '%%' || $%d || '%%'", filters.Keyword)
	}
	if filters.Status != "" {
		add("status = $%d", filters.Status)
	}
	if filters.DueFrom != nil {
		add("due_at_utc >= $%d", *filters.DueFrom)
	}
	if filters.DueTo != nil {
		add("due_at_utc <= $%d", *filters.DueTo)
	}
	if filters.NoDue {
		clauses = append(clauses, "due_at_utc is null")
	}
	args = append(args, limit)
	query := `
		select ` + todoColumns + `
		from todo.todos
		where ` + strings.Join(clauses, " and ") + `
		order by created_at asc
		limit $` + fmt.Sprint(len(args))
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTodos(rows)
}

// Dashboard returns the todo counters in one conditional-aggregation query.
// The reminder counters stay zero until ITER-0003 (D7).
func (s *Store) Dashboard(ctx context.Context, workspaceID, ownerUserID string, now, dueTodayStart, dueTodayEnd time.Time) (dto.DashboardSummary, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var summary dto.DashboardSummary
	err := exec.QueryRow(ctx, `
		select
		  count(*) filter (where status = 'pending'),
		  count(*) filter (where status = 'pending' and due_at_utc >= $3 and due_at_utc < $4),
		  count(*) filter (where status = 'pending' and due_at_utc < $5),
		  count(*) filter (where status = 'pending' and due_at_utc is null),
		  count(*) filter (where status = 'completed' and completed_at >= $6 and completed_at <= $5)
		from todo.todos
		where workspace_id = $1 and owner_user_id = $2
	`, workspaceID, ownerUserID, dueTodayStart, dueTodayEnd, now, now.Add(-7*24*time.Hour)).Scan(
		&summary.PendingTotal, &summary.DueToday, &summary.Overdue, &summary.NoDue, &summary.CompletedLast7Days,
	)
	return summary, err
}

// SearchCandidates returns pending todos whose title matches keyword
// (case-insensitively), capped at limit, each carrying its current version.
func (s *Store) SearchCandidates(ctx context.Context, workspaceID, ownerUserID, keyword string, limit int) ([]dto.Candidate, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select id, title, due_at_utc, version
		from todo.todos
		where workspace_id = $1 and owner_user_id = $2
		  and status = 'pending' and title ilike '%' || $3 || '%'
		order by created_at asc
		limit $4
	`, workspaceID, ownerUserID, keyword, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []dto.Candidate
	for rows.Next() {
		var candidate dto.Candidate
		if err := rows.Scan(&candidate.TodoID, &candidate.Title, &candidate.DueAtUTC, &candidate.Version); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

type todoRow interface {
	Scan(dest ...any) error
}

func scanTodo(row todoRow) (domain.Todo, error) {
	var todo domain.Todo
	var status string
	err := row.Scan(
		&todo.ID, &todo.WorkspaceID, &todo.OwnerUserID, &todo.Title, &todo.Description, &todo.DueAtUTC,
		&todo.TimezoneAtInput, &status, &todo.ReminderVersion, &todo.Version,
		&todo.CreatedAt, &todo.UpdatedAt, &todo.CompletedAt, &todo.DeletedAt,
	)
	if err != nil {
		return domain.Todo{}, err
	}
	todo.Status = domain.Status(status)
	return todo, nil
}

func collectTodos(rows pgx.Rows) ([]domain.Todo, error) {
	var todos []domain.Todo
	for rows.Next() {
		todo, err := scanTodo(rows)
		if err != nil {
			return nil, err
		}
		todos = append(todos, todo)
	}
	return todos, rows.Err()
}
