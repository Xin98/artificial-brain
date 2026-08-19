package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// OutboxRow is one reminder.fake_outbox record as read for the dev inbox.
// The inbound dev-inbox shape arrives in a later task; this row is the
// adapter-local read model.
type OutboxRow struct {
	Address   string
	Channel   string
	TodoID    string
	Body      string
	CreatedAt time.Time
}

// OutboxReader reads the reminder fake outbox for the gated dev inbox
// endpoint.
type OutboxReader struct {
	pool *pgxpool.Pool
}

// NewOutboxReader returns an OutboxReader bound to pool.
func NewOutboxReader(pool *pgxpool.Pool) *OutboxReader { return &OutboxReader{pool: pool} }

// LatestByAddress returns up to limit outbox rows for address, newest first.
func (r *OutboxReader) LatestByAddress(ctx context.Context, address string, limit int) ([]OutboxRow, error) {
	exec := database.ExecutorFromContextOr(ctx, r.pool)
	rows, err := exec.Query(ctx, `
		select address, channel, todo_id, body, created_at
		from reminder.fake_outbox
		where address = $1
		order by created_at desc, id desc
		limit $2
	`, address, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []OutboxRow
	for rows.Next() {
		var row OutboxRow
		if err := rows.Scan(&row.Address, &row.Channel, &row.TodoID, &row.Body, &row.CreatedAt); err != nil {
			return nil, err
		}
		// pgx scans timestamptz in the local location; report UTC instants.
		row.CreatedAt = row.CreatedAt.UTC()
		messages = append(messages, row)
	}
	return messages, rows.Err()
}
