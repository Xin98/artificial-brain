package domain

import "regexp"

var phonePattern = regexp.MustCompile(`^\+?[1-9][0-9]{6,14}$`)

// Phone is an E.164-like phone number used as the cloud login identifier.
type Phone string

func NewPhone(value string) (Phone, error) {
	if !phonePattern.MatchString(value) {
		return "", ErrInvalidPhone
	}
	return Phone(value), nil
}

func (p Phone) String() string { return string(p) }
