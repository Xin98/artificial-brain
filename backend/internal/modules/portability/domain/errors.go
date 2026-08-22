package domain

import "errors"

var (
	// ErrUnsupportedSchemaVersion marks a bundle manifest whose schema version
	// this build cannot read.
	ErrUnsupportedSchemaVersion = errors.New("portability: unsupported schema version")
	// ErrManifestInvalid marks a manifest with missing or malformed fields.
	ErrManifestInvalid = errors.New("portability: manifest invalid")
	// ErrRecordInvalid marks a bundle record that breaks an invariant; the
	// wrapping message names the record and the offending field.
	ErrRecordInvalid = errors.New("portability: record invalid")
	// ErrBundleStructure marks an archive with missing or unexpected entries.
	ErrBundleStructure = errors.New("portability: bundle structure invalid")
	// ErrChecksumMismatch marks an entry whose bytes no longer match the
	// manifest's recorded sha256.
	ErrChecksumMismatch = errors.New("portability: checksum mismatch")
	// ErrImportNotFound marks a lookup for an import that does not exist.
	ErrImportNotFound = errors.New("portability: import not found")
	// ErrImportConflict marks an operation on an import that is already
	// committed.
	ErrImportConflict = errors.New("portability: import already committed")
	// ErrImportExpired marks an operation on an import whose TTL has passed.
	ErrImportExpired = errors.New("portability: import expired")
	// ErrSourceRecordExists marks a source-record registration whose
	// (source instance, source record) pair was already imported.
	ErrSourceRecordExists = errors.New("portability: source record already exists")
)
