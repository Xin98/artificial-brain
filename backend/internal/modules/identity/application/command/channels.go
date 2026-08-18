package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// AddChannelHandler registers an unverified contact channel and sends a
// verification code via the outbound message port.
type AddChannelHandler struct {
	Channels ports.ChannelStore
	Outbox   ports.MessageOutbox
	NewCode  func() (string, error)
	NewID    func() string
	Now      func() time.Time
	CodeTTL  time.Duration
}

func (h *AddChannelHandler) Handle(ctx context.Context, principal dto.Principal, kind, address string) (dto.ContactChannelView, error) {
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

	code, err := h.NewCode()
	if err != nil {
		return dto.ContactChannelView{}, err
	}
	now := h.Now()
	expires := now.Add(h.CodeTTL)
	channel := domain.ContactChannel{
		ID:            h.NewID(),
		UserID:        principal.UserID,
		WorkspaceID:   principal.WorkspaceID,
		Kind:          k,
		Address:       address,
		Verified:      false,
		Enabled:       true,
		CodeHash:      domain.HashCode(code),
		CodeExpiresAt: &expires,
		CreatedAt:     now,
	}
	if err := h.Channels.Save(ctx, channel); err != nil {
		return dto.ContactChannelView{}, err
	}

	outboxChannel := "email"
	if k == domain.ChannelKindSMS {
		outboxChannel = "sms"
	}
	if err := h.Outbox.Write(ctx, ports.OutboxMessage{
		Address: address,
		Channel: outboxChannel,
		Purpose: "channel_verification",
		Code:    code,
	}); err != nil {
		return dto.ContactChannelView{}, err
	}
	return dto.ToChannelView(channel), nil
}

// VerifyChannelHandler marks a channel verified when the code matches.
type VerifyChannelHandler struct {
	Channels ports.ChannelStore
	Now      func() time.Time
}

func (h *VerifyChannelHandler) Handle(ctx context.Context, principal dto.Principal, channelID, code string) error {
	channel, err := h.Channels.ByID(ctx, principal.WorkspaceID, principal.UserID, channelID)
	if err != nil {
		return domain.ErrChannelNotFound
	}
	if _, err := domain.NewCode(code); err != nil {
		return err
	}
	if err := channel.Verify(domain.HashCode(code), h.Now()); err != nil {
		return err
	}
	return h.Channels.Update(ctx, channel)
}

// SetChannelEnabledHandler enables or disables a channel.
type SetChannelEnabledHandler struct {
	Channels ports.ChannelStore
}

func (h *SetChannelEnabledHandler) Handle(ctx context.Context, principal dto.Principal, channelID string, enabled bool) (dto.ContactChannelView, error) {
	channel, err := h.Channels.ByID(ctx, principal.WorkspaceID, principal.UserID, channelID)
	if err != nil {
		return dto.ContactChannelView{}, domain.ErrChannelNotFound
	}
	channel.SetEnabled(enabled)
	if err := h.Channels.Update(ctx, channel); err != nil {
		return dto.ContactChannelView{}, err
	}
	return dto.ToChannelView(channel), nil
}
