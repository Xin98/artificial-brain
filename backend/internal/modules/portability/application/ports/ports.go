// Package ports holds the Portability application's consumer-owned ports.
// Portability defines them; cmd/api adapts the other modules' public
// handlers, the instance identity seam, and the archive adapter to these
// interfaces. The portability application never imports another context.
package ports

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// ErrChannelExists is the portability-local sentinel for "a channel with
// this (user, kind, address) already exists". Portability may not import
// identity's domain, so the cmd shim translates identity's own
// ErrChannelExists into this sentinel — returning the existing channel's id
// alongside the error — and ConfirmImportHandler downgrades the decision to
// skipped while still registering the source record against that existing id.
var ErrChannelExists = errors.New("portability: channel already exists")

// Principal mirrors identity's authenticated subject shape without importing
// the identity context; cmd/api maps the real principal across.
type Principal struct {
	UserID      string
	WorkspaceID string
}

// TodoExporter pages the owner's full-history todos — pending, completed, and
// deleted — as export records.
type TodoExporter interface {
	ExportTodos(ctx context.Context, workspaceID, userID string, offset, limit int) ([]dto.TodoExportRecord, error)
}

// ChannelExporter returns the user's portable channel preferences; the result
// never carries verification state or code hashes.
type ChannelExporter interface {
	ExportChannels(ctx context.Context, principal Principal) ([]dto.ChannelExportRecord, error)
}

// DeliveryExporter pages the workspace's full delivery history — all states
// and both origins — as export records.
type DeliveryExporter interface {
	ExportDeliveries(ctx context.Context, workspaceID string, offset, limit int) ([]dto.DeliveryExportRecord, error)
}

// InstanceIdentityStore returns this instance's stable id; get-or-create is
// handled by the adapter.
type InstanceIdentityStore interface {
	InstanceID(ctx context.Context) (string, error) // get-or-create handled by the adapter
}

// ArchiveWriter streams bundle entries into an archive. WriteEntry tees
// sha256 per file and records the digest under the entry name; WriteManifest
// fills the manifest's Files map (shared by reference) from the recorded
// digests, then appends manifest.json last; Close finalizes the archive.
type ArchiveWriter interface {
	WriteEntry(ctx context.Context, name string, encode func(context.Context, io.Writer) error) error
	WriteManifest(ctx context.Context, manifest domain.Manifest) error
	Close() error
}

// ArchiveFactory builds an ArchiveWriter that streams into w.
type ArchiveFactory func(w io.Writer) ArchiveWriter

// ParsedBundle is a fully validated export bundle: the manifest plus every
// record decoded into its domain shape, in bundle order.
type ParsedBundle struct {
	Manifest   domain.Manifest
	Todos      []domain.TodoRecord
	Deliveries []domain.DeliveryRecord
	Channels   []domain.ChannelRecord
}

// BundleParser validates and decodes an export bundle's bytes. Parse checks
// structure, manifest, checksums, and records, and reports the typed domain
// errors — ErrBundleStructure, ErrChecksumMismatch,
// ErrUnsupportedSchemaVersion, ErrRecordInvalid — so callers never store or
// execute a bundle that failed validation.
type BundleParser interface {
	Parse(data []byte) (ParsedBundle, error)
}

// ImportStore persists the two-phase import lifecycle: the uploaded bundle
// row (state pending), and the confirm transition with its final report.
type ImportStore interface {
	Save(ctx context.Context, imp dto.ImportRecordRow) error
	// Get returns the row or domain.ErrImportNotFound.
	Get(ctx context.Context, workspaceID, importID string) (dto.ImportRecordRow, error)
	// Commit flips a pending row to committed with the report and commit
	// time; committing a row that is no longer pending reports
	// domain.ErrImportConflict.
	Commit(ctx context.Context, workspaceID, importID string, report dto.ImportReport, now time.Time) error
}

// SourceRecordStore is the Source Identity seam: which bundle records were
// already imported from which instance, and under which content fingerprint.
type SourceRecordStore interface {
	// Fingerprints returns the stored content fingerprints of the given
	// source record ids, keyed "sourceInstanceID:sourceRecordID"; ids never
	// imported are absent from the map.
	Fingerprints(ctx context.Context, sourceInstanceID string, ids []string) (map[string]string, error)
	// Targets returns the stored target ids of the given source record ids,
	// keyed like Fingerprints with the target row's id as value. Confirm uses
	// it to resolve a delivery's sourceTodoRecordId to the todo id a previous
	// import already created.
	Targets(ctx context.Context, sourceInstanceID string, ids []string) (map[string]string, error)
	// Register records one imported record; a duplicate
	// (sourceInstanceID, sourceRecordID) pair reports
	// domain.ErrSourceRecordExists.
	Register(ctx context.Context, record dto.SourceRecord) error
}

// TodoImporter restores one bundle todo through the todo module's public
// import command and returns the new todo's id.
type TodoImporter interface {
	ImportTodo(ctx context.Context, principal Principal, record dto.TodoImportRequest) (string, error)
}

// ChannelImporter restores one bundle channel through the identity module's
// public import command and returns the channel's id. A duplicate (user,
// kind, address) reports ErrChannelExists — the portability-local sentinel —
// with the EXISTING channel's id returned alongside the error.
type ChannelImporter interface {
	ImportChannel(ctx context.Context, principal Principal, record dto.ChannelImportRequest) (string, error)
}

// DeliveryImporter restores one bundle delivery history row through the
// reminder module's public import command.
type DeliveryImporter interface {
	ImportDelivery(ctx context.Context, principal Principal, record dto.DeliveryImportRequest) error
}

// UnitOfWork runs a unit of work atomically; it mirrors the todo module's
// port shape. cmd injects the joinable UoW so a confirm joins exactly one
// transaction — the handler composes importer and store calls inside Run and
// never begins a transaction itself.
type UnitOfWork interface {
	Run(ctx context.Context, work func(context.Context) error) error
}
