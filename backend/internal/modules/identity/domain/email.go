package domain

import "regexp"

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Email is a structurally validated email address used as a reminder channel.
type Email string

func NewEmail(value string) (Email, error) {
	if !emailPattern.MatchString(value) {
		return "", ErrInvalidEmail
	}
	return Email(value), nil
}

func (e Email) String() string { return string(e) }
