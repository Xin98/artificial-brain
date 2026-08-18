package domain

import "time"

// ChannelKind enumerates the supported contact channel types.
type ChannelKind string

const (
	ChannelKindEmail ChannelKind = "email"
	ChannelKindSMS   ChannelKind = "sms"
)

func NewChannelKind(value string) (ChannelKind, error) {
	switch ChannelKind(value) {
	case ChannelKindEmail, ChannelKindSMS:
		return ChannelKind(value), nil
	default:
		return "", ErrInvalidChannelKind
	}
}

// ContactChannel is a verified, enabled endpoint (email or SMS) usable for
// reminders. Verification codes are stored only as hashes.
type ContactChannel struct {
	ID            string
	UserID        string
	WorkspaceID   string
	Kind          ChannelKind
	Address       string
	Verified      bool
	Enabled       bool
	CodeHash      string
	CodeExpiresAt *time.Time
	CreatedAt     time.Time
}

// Verify marks the channel verified when the provided hash matches and the code
// has not expired.
func (c *ContactChannel) Verify(codeHash string, now time.Time) error {
	if c.CodeExpiresAt == nil || !now.Before(*c.CodeExpiresAt) {
		return ErrChannelCodeExpired
	}
	if c.CodeHash != codeHash {
		return ErrInvalidCode
	}
	c.Verified = true
	return nil
}

func (c *ContactChannel) SetEnabled(enabled bool) { c.Enabled = enabled }

// Usable reports whether the channel can receive reminders.
func (c *ContactChannel) Usable() bool { return c.Verified && c.Enabled }
