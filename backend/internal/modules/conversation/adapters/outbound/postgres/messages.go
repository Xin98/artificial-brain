package postgres

import (
	"context"
	"strconv"

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

// Append inserts one transcript row.
func (s *MessageLogStore) Append(ctx context.Context, message ports.MessageLog) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into conversation.messages
			(workspace_id, user_id, role, body, session_id, resolved_intent, created_at)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, message.WorkspaceID, message.UserID, message.Role, message.Body,
		message.SessionID, message.ResolvedIntent, message.CreatedAt)
	return err
}

// ListBySession returns the latest limit transcript rows of one session in
// ascending insertion order, scoped to the caller's workspace+user.
func (s *MessageLogStore) ListBySession(ctx context.Context, workspaceID, userID, sessionID string, limit int) ([]ports.MessageLogEntry, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select id, role, body, resolved_intent, created_at
		from conversation.messages
		where workspace_id = $1 and user_id = $2 and session_id = $3
		order by id desc
		limit $4
	`, workspaceID, userID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reversed []ports.MessageLogEntry
	for rows.Next() {
		var entry ports.MessageLogEntry
		var id int64
		if err := rows.Scan(&id, &entry.Role, &entry.Body,
			&entry.ResolvedIntent, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entry.ID = strconv.FormatInt(id, 10)
		entry.SessionID = sessionID
		reversed = append(reversed, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The query took the latest rows descending; history reads ascending.
	messages := make([]ports.MessageLogEntry, 0, len(reversed))
	for index := len(reversed) - 1; index >= 0; index-- {
		messages = append(messages, reversed[index])
	}
	return messages, nil
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
