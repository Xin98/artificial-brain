package domain

import "time"

// MaxVerifyAttempts bounds how many wrong codes a single challenge accepts
// before it is invalidated.
const MaxVerifyAttempts = 5

// LoginChallenge is a short-lived, single-use request to verify a phone number
// with a six-digit code. Only the code hash is stored.
type LoginChallenge struct {
	ID         string
	Phone      string
	CodeHash   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	Attempts   int
}

func (c *LoginChallenge) IsExpired(now time.Time) bool { return !now.Before(c.ExpiresAt) }

func (c *LoginChallenge) IsConsumed() bool { return c.ConsumedAt != nil }

// Consume marks the challenge used. A consumed challenge cannot be reused.
func (c *LoginChallenge) Consume(now time.Time) error {
	if c.IsConsumed() {
		return ErrChallengeConsumed
	}
	c.ConsumedAt = &now
	return nil
}

// Matches reports whether the provided code hash equals the stored hash.
func (c *LoginChallenge) Matches(codeHash string) bool { return c.CodeHash == codeHash }

// RegisterFailedAttempt increments the attempt counter and reports whether
// the challenge is now exhausted.
func (c *LoginChallenge) RegisterFailedAttempt() bool {
	c.Attempts++
	return c.Attempts >= MaxVerifyAttempts
}
