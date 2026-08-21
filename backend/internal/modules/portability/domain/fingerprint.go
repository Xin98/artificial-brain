package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fingerprint returns the sha256 hex digest of the record's canonical JSON.
// The record is round-tripped through map[string]any, and encoding/json sorts
// map keys on marshal, so the digest never depends on field or insertion
// order. Records that cannot marshal to a JSON object fall back to their raw
// marshal bytes; records that cannot marshal at all return "".
func Fingerprint(record any) string {
	data, err := json.Marshal(record)
	if err != nil {
		return ""
	}
	var canonical map[string]any
	if err := json.Unmarshal(data, &canonical); err == nil {
		if sorted, err := json.Marshal(canonical); err == nil {
			data = sorted
		}
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
