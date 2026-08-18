package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/inbound/http"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// OutboxReader reads the fake outbox for the gated dev inbox endpoint.
type OutboxReader struct {
	pool *pgxpool.Pool
}

func NewOutboxReader(pool *pgxpool.Pool) *OutboxReader { return &OutboxReader{pool: pool} }

func (r *OutboxReader) LatestByAddress(ctx context.Context, address string, limit int) ([]httpadapter.DevInboxMessage, error) {
	exec := database.ExecutorFromContextOr(ctx, r.pool)
	rows, err := exec.Query(ctx, `
		select address, channel, purpose, code, created_at
		from identity.message_outbox
		where address = $1
		order by created_at desc, id desc
		limit $2
	`, address, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []httpadapter.DevInboxMessage
	for rows.Next() {
		var m httpadapter.DevInboxMessage
		if err := rows.Scan(&m.Address, &m.Channel, &m.Purpose, &m.Code, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
