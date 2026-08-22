package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// ImportChannelHandler restores a contact channel from a portable export
// during private-deployment data import.
type ImportChannelHandler struct {
	Channels ports.ChannelStore
	NewID    func() string
	Now      func() time.Time
}

// Handle creates an UNVERIFIED channel (no code state) with Enabled as
// requested; a duplicate (user, kind, address) returns domain.ErrChannelExists
// for the caller to classify.
func (h *ImportChannelHandler) Handle(ctx context.Context, principal dto.Principal, kind, address string, enabled bool) (dto.ContactChannelView, error) {
	k, err := domain.NewChannelKind(kind)
	if err != nil {
		return dto.ContactChannelView{}, err
	}
	switch k {
	case domain.ChannelKindEmail:
		if _, err := domain.NewEmail(address); err != nil {
			return dto.ContactChannelView{}, err
		}
	case domain.ChannelKindSMS:
		if _, err := domain.NewPhone(address); err != nil {
			return dto.ContactChannelView{}, err
		}
	}

	channel := domain.ContactChannel{
		ID:          h.NewID(),
		UserID:      principal.UserID,
		WorkspaceID: principal.WorkspaceID,
		Kind:        k,
		Address:     address,
		Verified:    false,
		Enabled:     enabled,
		CreatedAt:   h.Now(),
	}
	if err := h.Channels.Save(ctx, channel); err != nil {
		return dto.ContactChannelView{}, err
	}
	return dto.ToChannelView(channel), nil
}
