package domain

import "time"

// Session is an authenticated server-side session. Only the SHA-256 hash of the
// bearer token is stored; the token itself is returned once at login.
type Session struct {
	ID          string
	UserID      string
	WorkspaceID string
	TokenHash   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

func (s *Session) IsExpired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

func (s *Session) IsRevoked() bool { return s.RevokedAt != nil }

// IsActive reports whether the session can authenticate a request at now.
func (s *Session) IsActive(now time.Time) bool {
	return !s.IsRevoked() && !s.IsExpired(now)
}

// Revoke invalidates the session.
func (s *Session) Revoke(now time.Time) {
	if s.RevokedAt == nil {
		s.RevokedAt = &now
	}
}
