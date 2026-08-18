package dto

import (
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// ContactChannelView is the read model for a contact channel.
type ContactChannelView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Address   string    `json:"address"`
	Verified  bool      `json:"verified"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

// ToChannelView maps a domain contact channel to its read model.
func ToChannelView(channel domain.ContactChannel) ContactChannelView {
	return ContactChannelView{
		ID:        channel.ID,
		Kind:      string(channel.Kind),
		Address:   channel.Address,
		Verified:  channel.Verified,
		Enabled:   channel.Enabled,
		CreatedAt: channel.CreatedAt,
	}
}
