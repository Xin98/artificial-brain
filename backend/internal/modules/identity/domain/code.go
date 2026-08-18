package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

var codePattern = regexp.MustCompile(`^[0-9]{6}$`)

// Code is a six-digit one-time code. Only its SHA-256 hash is ever persisted.
type Code string

func NewCode(value string) (Code, error) {
	if !codePattern.MatchString(value) {
		return "", ErrInvalidCode
	}
	return Code(value), nil
}

func (c Code) String() string { return string(c) }

// HashCode returns the SHA-256 hex digest of a code. Challenge and channel code
// storage keeps only this digest, never the plaintext.
func HashCode(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
