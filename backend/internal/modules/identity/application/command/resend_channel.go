package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// ChannelResendCooldown bounds verification delivery requests per channel.
const ChannelResendCooldown = time.Minute

// ResendChannelHandler sends a fresh code for an existing unverified channel.
// Only the hash is persisted. A failed send keeps the previous code usable.
type ResendChannelHandler struct {
	Channels ports.ChannelStore
	Outbox   ports.MessageOutbox
	NewCode  func() (string, error)
	Now      func() time.Time
	CodeTTL  time.Duration
	UoW      ports.VerificationUnitOfWork
}

func (h *ResendChannelHandler) Handle(ctx context.Context, principal dto.Principal, channelID string) error {
	return h.UoW.Run(ctx, func(ctx context.Context) error {
		var channel domain.ContactChannel
		var err error
		if locked, ok := h.Channels.(ports.ChannelVerificationStore); ok {
			channel, err = locked.ByIDForUpdate(ctx, principal.WorkspaceID, principal.UserID, channelID)
		} else {
			channel, err = h.Channels.ByID(ctx, principal.WorkspaceID, principal.UserID, channelID)
		}
		if err != nil {
			return err
		}
		if channel.Verified {
			return domain.ErrChannelAlreadyVerified
		}
		now := h.Now()
		if channel.CodeExpiresAt != nil && now.Before(channel.CodeExpiresAt.Add(-h.CodeTTL).Add(ChannelResendCooldown)) {
			return domain.ErrRateLimited
		}
		code, err := h.NewCode()
		if err != nil {
			return err
		}
		if _, err := domain.NewCode(code); err != nil {
			return err
		}
		if err := h.Outbox.Write(ctx, ports.OutboxMessage{Address: channel.Address, Channel: string(channel.Kind), Purpose: "channel_verification", Code: code}); err != nil {
			return err
		}
		expires := now.Add(h.CodeTTL)
		channel.CodeHash, channel.CodeExpiresAt = domain.HashCode(code), &expires
		return h.Channels.Update(ctx, channel)
	})
}
