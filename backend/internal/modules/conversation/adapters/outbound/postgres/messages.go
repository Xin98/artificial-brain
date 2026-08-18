package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// MessageLogStore appends conversation audit rows.
type MessageLogStore struct {
	pool *pgxpool.Pool
}

var _ ports.MessageLogStore = (*MessageLogStore)(nil)

// NewMessageLogStore returns a MessageLogStore bound to pool.
func NewMessageLogStore(pool *pgxpool.Pool) *MessageLogStore {
	return &MessageLogStore{pool: pool}
}

// Append inserts one audit row.
func (s *MessageLogStore) Append(ctx context.Context, message ports.MessageLog) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into conversation.messages
			(workspace_id, user_id, role, body, resolved_intent, created_at)
		values ($1, $2, $3, $4, $5, $6)
	`, message.WorkspaceID, message.UserID, message.Role, message.Body,
		message.ResolvedIntent, message.CreatedAt)
	return err
}

// ListByUser returns the caller's audit rows in insertion order. It is used
// by tests and future export seams; it is not part of the port.
func (s *MessageLogStore) ListByUser(ctx context.Context, workspaceID, userID string) ([]ports.MessageLog, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select workspace_id, user_id, role, body, resolved_intent, created_at
		from conversation.messages
		where workspace_id = $1 and user_id = $2
		order by id asc
	`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ports.MessageLog
	for rows.Next() {
		var message ports.MessageLog
		if err := rows.Scan(&message.WorkspaceID, &message.UserID, &message.Role,
			&message.Body, &message.ResolvedIntent, &message.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
