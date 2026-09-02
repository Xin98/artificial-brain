package domain

import (
	"regexp"
	"strings"
)

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Email is a structurally validated, case-normalized email address used as a
// login identifier or reminder channel.
type Email string

// NewEmail validates and case-normalizes value: the local part is
// case-insensitive for every provider we target, so every consumer compares
// one canonical form (the users(email) unique index mirrors this via
// lower(email)).
func NewEmail(value string) (Email, error) {
	if !emailPattern.MatchString(value) {
		return "", ErrInvalidEmail
	}
	return Email(strings.ToLower(value)), nil
}

func (e Email) String() string { return string(e) }
