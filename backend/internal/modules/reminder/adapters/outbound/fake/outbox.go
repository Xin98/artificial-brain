// Package fake is the ITER-0003 fake reminder provider outbox: instead of
// calling a real SMS/email provider, the fake notifier adapters record the
// rendered reminder bodies into reminder.fake_outbox, where the gated dev
// inbox reads them.
package fake

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// Outbox writes rendered reminder messages to reminder.fake_outbox.
type Outbox struct {
	pool *pgxpool.Pool
}

// NewOutbox returns an Outbox bound to pool.
func NewOutbox(pool *pgxpool.Pool) *Outbox { return &Outbox{pool: pool} }

// Write records one outbound message, joining the caller's ambient
// transaction so a failed send rolls the record back with it.
func (o *Outbox) Write(ctx context.Context, channel, address, todoID, body string) error {
	exec := database.ExecutorFromContextOr(ctx, o.pool)
	_, err := exec.Exec(ctx, `
		insert into reminder.fake_outbox (address, channel, todo_id, body)
		values ($1, $2, $3, $4)
	`, address, channel, todoID, body)
	return err
}
