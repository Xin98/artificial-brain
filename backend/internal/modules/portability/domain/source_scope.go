package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

func OwnerSourceNamespace(workspace, user, source string) string {
	sum := sha256.Sum256([]byte(workspace + "\x00" + user + "\x00" + source))
	return "owner:" + hex.EncodeToString(sum[:])
}
