package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
)

// ChannelsExportQuery produces the portable, privacy-safe channel export for
// private-deployment data portability.
type ChannelsExportQuery struct{ Channels ports.ChannelStore }

// GetChannelPreferences returns kind/address/enabled only — never codes or
// verification state.
func (q *ChannelsExportQuery) GetChannelPreferences(ctx context.Context, principal dto.Principal) ([]dto.ChannelPreference, error) {
	channels, err := q.Channels.ListByUser(ctx, principal.WorkspaceID, principal.UserID)
	if err != nil {
		return nil, err
	}
	prefs := make([]dto.ChannelPreference, 0, len(channels))
	for _, channel := range channels {
		prefs = append(prefs, dto.ChannelPreference{
			ID:      channel.ID,
			Kind:    string(channel.Kind),
			Address: channel.Address,
			Enabled: channel.Enabled,
		})
	}
	return prefs, nil
}
