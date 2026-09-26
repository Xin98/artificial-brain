package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// SessionStore persists conversation sessions. Writes resolve their executor
// from context so they join the caller's ambient transaction. Every accessor
// is scoped to workspace+user; scoped misses are domain.ErrSessionNotFound so
// cross-tenant probes cannot distinguish existence.
type SessionStore struct {
	pool *pgxpool.Pool
}

var _ ports.SessionStore = (*SessionStore)(nil)

// NewSessionStore returns a SessionStore bound to pool.
func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{pool: pool}
}

// Create inserts one session row.
func (s *SessionStore) Create(ctx context.Context, session domain.Session) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into conversation.sessions
			(id, workspace_id, user_id, title, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6)
	`, session.ID, session.WorkspaceID, session.UserID, session.Title,
		session.CreatedAt, session.UpdatedAt)
	return err
}

// Get loads one session scoped to the caller.
func (s *SessionStore) Get(ctx context.Context, workspaceID, userID, sessionID string) (domain.Session, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var session domain.Session
	err := exec.QueryRow(ctx, `
		select id, workspace_id, user_id, title, created_at, updated_at
		from conversation.sessions
		where id = $1 and workspace_id = $2 and user_id = $3
	`, sessionID, workspaceID, userID).Scan(
		&session.ID, &session.WorkspaceID, &session.UserID,
		&session.Title, &session.CreatedAt, &session.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrSessionNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

// List returns up to limit sessions ordered by UpdatedAt descending; ties
// break on the id so paging-free listings stay stable.
func (s *SessionStore) List(ctx context.Context, workspaceID, userID string, limit int) ([]domain.Session, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select id, workspace_id, user_id, title, created_at, updated_at
		from conversation.sessions
		where workspace_id = $1 and user_id = $2
		order by updated_at desc, id desc
		limit $3
	`, workspaceID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.Session
	for rows.Next() {
		var session domain.Session
		if err := rows.Scan(&session.ID, &session.WorkspaceID, &session.UserID,
			&session.Title, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// Rename sets the validated title and advances UpdatedAt. The conditional
// update's rows-affected count turns a scoped miss into ErrSessionNotFound.
func (s *SessionStore) Rename(ctx context.Context, workspaceID, userID, sessionID, title string, now time.Time) error {
	return s.scopedUpdate(ctx, `
		update conversation.sessions
		set title = $4, updated_at = $5
		where id = $1 and workspace_id = $2 and user_id = $3
	`, sessionID, workspaceID, userID, title, now)
}

// Delete removes the session; the cascading foreign key removes its
// transcript rows in the same statement.
func (s *SessionStore) Delete(ctx context.Context, workspaceID, userID, sessionID string) error {
	return s.scopedUpdate(ctx, `
		delete from conversation.sessions
		where id = $1 and workspace_id = $2 and user_id = $3
	`, sessionID, workspaceID, userID)
}

// Touch advances UpdatedAt so the sidebar re-orders on activity.
func (s *SessionStore) Touch(ctx context.Context, workspaceID, userID, sessionID string, now time.Time) error {
	return s.scopedUpdate(ctx, `
		update conversation.sessions
		set updated_at = $4
		where id = $1 and workspace_id = $2 and user_id = $3
	`, sessionID, workspaceID, userID, now)
}

func (s *SessionStore) scopedUpdate(ctx context.Context, query, sessionID, workspaceID, userID string, args ...any) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	tag, err := exec.Exec(ctx, query, append([]any{sessionID, workspaceID, userID}, args...)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrSessionNotFound
	}
	return nil
}
