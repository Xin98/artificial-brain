// Package ports holds the Portability application's consumer-owned ports.
// Portability defines them; cmd/api adapts the other modules' public
// handlers, the instance identity seam, and the archive adapter to these
// interfaces. The portability application never imports another context.
package ports

import (
	"context"
	"io"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

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
