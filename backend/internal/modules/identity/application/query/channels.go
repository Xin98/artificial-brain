package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
)

// ChannelsQuery lists a user's contact channels.
type ChannelsQuery struct {
	Channels ports.ChannelStore
}

func (q *ChannelsQuery) GetContactChannels(ctx context.Context, principal dto.Principal) ([]dto.ContactChannelView, error) {
	channels, err := q.Channels.ListByUser(ctx, principal.WorkspaceID, principal.UserID)
	if err != nil {
		return nil, err
	}
	views := make([]dto.ContactChannelView, 0, len(channels))
	for _, channel := range channels {
		views = append(views, dto.ToChannelView(channel))
	}
	return views, nil
}
