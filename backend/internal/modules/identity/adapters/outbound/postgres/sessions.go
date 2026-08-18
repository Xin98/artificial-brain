package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// SessionStore persists sessions in PostgreSQL.
type SessionStore struct {
	pool *pgxpool.Pool
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore { return &SessionStore{pool: pool} }

func (s *SessionStore) Save(ctx context.Context, session domain.Session) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.sessions
			(id, user_id, workspace_id, token_hash, created_at, expires_at, revoked_at)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, session.ID, session.UserID, session.WorkspaceID, session.TokenHash,
		session.CreatedAt, session.ExpiresAt, session.RevokedAt)
	return err
}

func (s *SessionStore) Update(ctx context.Context, session domain.Session) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		update identity.sessions
		set expires_at = $2, revoked_at = $3
		where id = $1
	`, session.ID, session.ExpiresAt, session.RevokedAt)
	return err
}

func (s *SessionStore) ByID(ctx context.Context, sessionID string) (domain.Session, error) {
	return s.query(ctx, "where id = $1", sessionID)
}

func (s *SessionStore) ByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	return s.query(ctx, "where token_hash = $1", tokenHash)
}

func (s *SessionStore) query(ctx context.Context, where string, arg any) (domain.Session, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var session domain.Session
	err := exec.QueryRow(ctx, `
		select id, user_id, workspace_id, token_hash, created_at, expires_at, revoked_at
		from identity.sessions
		`+where, arg).Scan(
		&session.ID, &session.UserID, &session.WorkspaceID, &session.TokenHash,
		&session.CreatedAt, &session.ExpiresAt, &session.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrSessionNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	return session, nil
}
