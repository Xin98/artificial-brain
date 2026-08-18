// Package fakeoutbox is the ITER-0002 fake notification adapter. It records
// outbound messages (login codes and channel-verification codes) into the dev
// outbox table instead of calling a real SMS/email provider. Plaintext codes
// exist only in this table; all other code storage is hash-only.
package fakeoutbox

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// Outbox writes messages to identity.message_outbox.
type Outbox struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Outbox { return &Outbox{pool: pool} }

func (o *Outbox) Write(ctx context.Context, message ports.OutboxMessage) error {
	exec := database.ExecutorFromContextOr(ctx, o.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.message_outbox (address, channel, purpose, code)
		values ($1, $2, $3, $4)
	`, message.Address, message.Channel, message.Purpose, message.Code)
	return err
}
