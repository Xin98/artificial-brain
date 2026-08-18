package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// ChannelStore persists contact channels in PostgreSQL.
type ChannelStore struct {
	pool *pgxpool.Pool
}

func NewChannelStore(pool *pgxpool.Pool) *ChannelStore { return &ChannelStore{pool: pool} }

func (s *ChannelStore) Save(ctx context.Context, channel domain.ContactChannel) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.contact_channels
			(id, user_id, workspace_id, kind, address, verified, enabled,
			 code_hash, code_expires_at, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, channel.ID, channel.UserID, channel.WorkspaceID, string(channel.Kind),
		channel.Address, channel.Verified, channel.Enabled,
		channel.CodeHash, channel.CodeExpiresAt, channel.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrChannelExists
	}
	return err
}

func (s *ChannelStore) Update(ctx context.Context, channel domain.ContactChannel) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		update identity.contact_channels
		set verified = $2, enabled = $3, code_hash = $4, code_expires_at = $5
		where id = $1
	`, channel.ID, channel.Verified, channel.Enabled, channel.CodeHash, channel.CodeExpiresAt)
	return err
}

func (s *ChannelStore) ByID(ctx context.Context, workspaceID, userID, channelID string) (domain.ContactChannel, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	channel, err := scanChannel(exec.QueryRow(ctx, `
		select id, user_id, workspace_id, kind, address, verified, enabled,
		       code_hash, code_expires_at, created_at
		from identity.contact_channels
		where id = $1 and user_id = $2 and workspace_id = $3
	`, channelID, userID, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContactChannel{}, domain.ErrChannelNotFound
	}
	return channel, err
}

func (s *ChannelStore) ListByUser(ctx context.Context, workspaceID, userID string) ([]domain.ContactChannel, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select id, user_id, workspace_id, kind, address, verified, enabled,
		       code_hash, code_expires_at, created_at
		from identity.contact_channels
		where user_id = $1 and workspace_id = $2
		order by created_at asc
	`, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []domain.ContactChannel
	for rows.Next() {
		channel, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

const uniqueViolation = "23505"

type channelRow interface {
	Scan(dest ...any) error
}

func scanChannel(row channelRow) (domain.ContactChannel, error) {
	var channel domain.ContactChannel
	var kind string
	var codeHash *string
	err := row.Scan(
		&channel.ID, &channel.UserID, &channel.WorkspaceID, &kind, &channel.Address,
		&channel.Verified, &channel.Enabled, &codeHash, &channel.CodeExpiresAt,
		&channel.CreatedAt,
	)
	if err != nil {
		return domain.ContactChannel{}, err
	}
	channel.Kind = domain.ChannelKind(kind)
	if codeHash != nil {
		channel.CodeHash = *codeHash
	}
	return channel, nil
}
