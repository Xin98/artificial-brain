package postgres

import (
	"context"
	"errors"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
)

// SetEnabled updates only enabled and returns the current channel snapshot.
// The UPDATE waits on any verification/resend row lock and cannot overwrite
// their code hash, expiry, or verified flag with an earlier snapshot.
func (s *ChannelStore) SetEnabled(ctx context.Context, workspaceID, userID, channelID string, enabled bool) (domain.ContactChannel, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	channel, err := scanChannel(exec.QueryRow(ctx, `
		update identity.contact_channels
		set enabled = $4
		where id = $1 and user_id = $2 and workspace_id = $3
		returning id, user_id, workspace_id, kind, address, verified, enabled,
		          code_hash, code_expires_at, created_at
	`, channelID, userID, workspaceID, enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ContactChannel{}, domain.ErrChannelNotFound
	}
	return channel, err
}
