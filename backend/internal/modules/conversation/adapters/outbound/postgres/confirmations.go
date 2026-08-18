// Package postgres implements the Conversation stores on PostgreSQL.
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

// ConfirmationStore persists confirmation requests. Writes resolve their
// executor from context so they join the caller's ambient transaction.
type ConfirmationStore struct {
	pool *pgxpool.Pool
}

var _ ports.ConfirmationStore = (*ConfirmationStore)(nil)

// NewConfirmationStore returns a ConfirmationStore bound to pool.
func NewConfirmationStore(pool *pgxpool.Pool) *ConfirmationStore {
	return &ConfirmationStore{pool: pool}
}

// Save inserts a confirmation request.
func (s *ConfirmationStore) Save(ctx context.Context, confirmation domain.ConfirmationRequest) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into conversation.confirmation_requests
			(id, workspace_id, user_id, intent, todo_id, todo_version,
			 created_at, expires_at, consumed_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, confirmation.ID, confirmation.WorkspaceID, confirmation.UserID,
		string(confirmation.Intent), confirmation.TodoID, confirmation.TodoVersion,
		confirmation.CreatedAt, confirmation.ExpiresAt, confirmation.ConsumedAt)
	return err
}

// Get loads a confirmation scoped to the caller.
func (s *ConfirmationStore) Get(ctx context.Context, workspaceID, userID, confirmationID string) (domain.ConfirmationRequest, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var confirmation domain.ConfirmationRequest
	var intent string
	err := exec.QueryRow(ctx, `
		select id, workspace_id, user_id, intent, todo_id, todo_version,
		       created_at, expires_at, consumed_at
		from conversation.confirmation_requests
		where id = $1 and workspace_id = $2 and user_id = $3
	`, confirmationID, workspaceID, userID).Scan(
		&confirmation.ID, &confirmation.WorkspaceID, &confirmation.UserID,
		&intent, &confirmation.TodoID, &confirmation.TodoVersion,
		&confirmation.CreatedAt, &confirmation.ExpiresAt, &confirmation.ConsumedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConfirmationRequest{}, domain.ErrConfirmationNotFound
	}
	if err != nil {
		return domain.ConfirmationRequest{}, err
	}
	confirmation.Intent = domain.Intent(intent)
	return confirmation, nil
}

// Consume atomically marks an unconsumed, unexpired confirmation consumed.
// The conditional update makes double-confirms impossible even under
// concurrency; a miss is classified as consumed or expired.
func (s *ConfirmationStore) Consume(ctx context.Context, workspaceID, userID, confirmationID string, now time.Time) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	tag, err := exec.Exec(ctx, `
		update conversation.confirmation_requests
		set consumed_at = $4
		where id = $1 and workspace_id = $2 and user_id = $3
		  and consumed_at is null and expires_at > $4
	`, confirmationID, workspaceID, userID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var consumed bool
	err = exec.QueryRow(ctx, `
		select consumed_at is not null
		from conversation.confirmation_requests
		where id = $1 and workspace_id = $2 and user_id = $3
	`, confirmationID, workspaceID, userID).Scan(&consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConfirmationNotFound
	}
	if err != nil {
		return err
	}
	if consumed {
		return domain.ErrConfirmationConsumed
	}
	return domain.ErrConfirmationExpired
}
