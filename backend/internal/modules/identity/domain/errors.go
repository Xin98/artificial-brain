package domain

import "errors"

var (
	ErrInvalidPhone       = errors.New("identity: invalid phone number")
	ErrInvalidEmail       = errors.New("identity: invalid email address")
	ErrInvalidCode        = errors.New("identity: code does not match")
	ErrChallengeNotFound  = errors.New("identity: login challenge not found")
	ErrChallengeExpired   = errors.New("identity: login challenge expired")
	ErrChallengeConsumed  = errors.New("identity: login challenge already used")
	ErrTooManyAttempts    = errors.New("identity: too many verification attempts")
	ErrRateLimited        = errors.New("identity: too many requests")
	ErrUserNotFound       = errors.New("identity: user not found")
	ErrSessionNotFound    = errors.New("identity: session not found")
	ErrSessionInactive    = errors.New("identity: session is not active")
	ErrChannelNotFound    = errors.New("identity: contact channel not found")
	ErrChannelExists      = errors.New("identity: contact channel already exists")
	ErrInvalidChannelKind = errors.New("identity: invalid contact channel kind")
	ErrChannelCodeExpired = errors.New("identity: contact channel code expired")
)
