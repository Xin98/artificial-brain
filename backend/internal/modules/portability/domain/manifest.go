// Package domain holds the Portability context's export/import invariants:
// bundle manifest validation, record validation, the import decision engine,
// and content fingerprints. It is pure stdlib and performs no I/O.
package domain

import (
	"fmt"
	"time"
)

// SchemaVersion identifies the export bundle format this build reads and
// writes. Bumping it is a compatibility decision, never silent.
const SchemaVersion = "1"

// Manifest describes an export bundle: which instance produced it, when,
// how many records it carries, and the sha256 hex digest of every entry
// file. The manifest itself is never listed in Files.
type Manifest struct {
	SchemaVersion    string
	SourceInstanceID string
	ExportedAt       time.Time
	Counts           ManifestCounts
	Files            map[string]string // filename -> sha256 hex of the entry
}

// ManifestCounts records how many records of each kind the bundle carries.
type ManifestCounts struct {
	Todos      int
	Deliveries int
	Channels   int
}

// ValidateManifest checks the manifest invariants. The schema version is
// checked first so unsupported bundles fail before any field is trusted;
// every other violation reports ErrManifestInvalid.
func ValidateManifest(m Manifest) error {
	if m.SchemaVersion != SchemaVersion {
		return ErrUnsupportedSchemaVersion
	}
	if m.SourceInstanceID == "" {
		return fmt.Errorf("%w: source instance id is required", ErrManifestInvalid)
	}
	if m.ExportedAt.IsZero() {
		return fmt.Errorf("%w: exported at is required", ErrManifestInvalid)
	}
	if m.Counts.Todos < 0 || m.Counts.Deliveries < 0 || m.Counts.Channels < 0 {
		return fmt.Errorf("%w: counts must not be negative", ErrManifestInvalid)
	}
	if len(m.Files) == 0 {
		return fmt.Errorf("%w: files must not be empty", ErrManifestInvalid)
	}
	for name, checksum := range m.Files {
		if name == "" {
			return fmt.Errorf("%w: file name must not be empty", ErrManifestInvalid)
		}
		if checksum == "" {
			return fmt.Errorf("%w: checksum for file %q must not be empty", ErrManifestInvalid, name)
		}
	}
	return nil
}
