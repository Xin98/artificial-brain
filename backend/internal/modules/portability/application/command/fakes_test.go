package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

var (
	errInstanceFailed   = errors.New("instance lookup failed")
	errTodosFailed      = errors.New("todo export failed")
	errDeliveriesFailed = errors.New("delivery export failed")
	errChannelsFailed   = errors.New("channel export failed")
	errArchiveEntry     = errors.New("archive entry failed")
	errArchiveManifest  = errors.New("archive manifest failed")
	errArchiveClose     = errors.New("archive close failed")
)

// pageCall records one paged exporter call.
type pageCall struct {
	workspaceID string
	userID      string
	offset      int
	limit       int
}

// fakeTodoExporter serves fixed pages keyed by offset/limit, so repeated
// passes (the JSON and CSV streams) page the same source; calls past the last
// page return an empty page. failAt fails that call index (0-based) and every
// call after it; -1 never fails.
type fakeTodoExporter struct {
	pages  [][]dto.TodoExportRecord
	failAt int
	calls  []pageCall
}

func (f *fakeTodoExporter) ExportTodos(_ context.Context, workspaceID, userID string, offset, limit int) ([]dto.TodoExportRecord, error) {
	f.calls = append(f.calls, pageCall{workspaceID: workspaceID, userID: userID, offset: offset, limit: limit})
	if f.failAt >= 0 && len(f.calls)-1 >= f.failAt {
		return nil, errTodosFailed
	}
	if limit <= 0 || offset/limit >= len(f.pages) {
		return []dto.TodoExportRecord{}, nil
	}
	return f.pages[offset/limit], nil
}

// fakeDeliveryExporter mirrors fakeTodoExporter for the delivery history.
type fakeDeliveryExporter struct {
	pages  [][]dto.DeliveryExportRecord
	failAt int
	calls  []pageCall
}

func (f *fakeDeliveryExporter) ExportDeliveries(_ context.Context, workspaceID string, offset, limit int) ([]dto.DeliveryExportRecord, error) {
	f.calls = append(f.calls, pageCall{workspaceID: workspaceID, offset: offset, limit: limit})
	if f.failAt >= 0 && len(f.calls)-1 >= f.failAt {
		return nil, errDeliveriesFailed
	}
	if limit <= 0 || offset/limit >= len(f.pages) {
		return []dto.DeliveryExportRecord{}, nil
	}
	return f.pages[offset/limit], nil
}

// fakeChannelExporter serves one fixed channel list and records the principal.
type fakeChannelExporter struct {
	records      []dto.ChannelExportRecord
	err          error
	calls        int
	gotPrincipal ports.Principal
}

func (f *fakeChannelExporter) ExportChannels(_ context.Context, principal ports.Principal) ([]dto.ChannelExportRecord, error) {
	f.calls++
	f.gotPrincipal = principal
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

// fakeInstanceStore returns a fixed instance id or error.
type fakeInstanceStore struct {
	id    string
	err   error
	calls int
}

func (f *fakeInstanceStore) InstanceID(_ context.Context) (string, error) {
	f.calls++
	return f.id, f.err
}

// fakeArchive captures entry names, order, and bytes in memory. It mirrors
// the production archive contract: WriteEntry records the sha256 of every
// streamed entry, and WriteManifest fills the manifest's shared Files map
// with those digests before appending manifest.json.
type fakeArchive struct {
	entries      []string
	data         map[string][]byte
	hashes       map[string]string
	manifest     *domain.Manifest
	manifestDone bool
	closed       bool
	entryErr     error
	manifestErr  error
	closeErr     error
}

func newFakeArchive() *fakeArchive {
	return &fakeArchive{
		data:   map[string][]byte{},
		hashes: map[string]string{},
	}
}

func (a *fakeArchive) WriteEntry(_ context.Context, name string, encode func(context.Context, io.Writer) error) error {
	if a.entryErr != nil {
		return a.entryErr
	}
	var buf bytes.Buffer
	if err := encode(context.Background(), &buf); err != nil {
		return err
	}
	a.entries = append(a.entries, name)
	a.data[name] = buf.Bytes()
	sum := sha256.Sum256(buf.Bytes())
	a.hashes[name] = hex.EncodeToString(sum[:])
	return nil
}

func (a *fakeArchive) WriteManifest(_ context.Context, manifest domain.Manifest) error {
	if a.manifestErr != nil {
		return a.manifestErr
	}
	if manifest.Files == nil {
		return errors.New("manifest files map must be non-nil: the archive fills the shared map")
	}
	for name, checksum := range a.hashes {
		manifest.Files[name] = checksum
	}
	wire, err := json.Marshal(dto.NewBundleManifest(manifest))
	if err != nil {
		return err
	}
	a.entries = append(a.entries, dto.ManifestEntry)
	a.data[dto.ManifestEntry] = wire
	captured := manifest
	a.manifest = &captured
	a.manifestDone = true
	return nil
}

func (a *fakeArchive) Close() error {
	a.closed = true
	return a.closeErr
}

// fakeArchiveFactory records the writer it was given and returns the fixed
// fake archive.
type fakeArchiveFactory struct {
	archive   *fakeArchive
	calls     int
	gotWriter io.Writer
}

func (f *fakeArchiveFactory) newArchive(w io.Writer) ports.ArchiveWriter {
	f.calls++
	f.gotWriter = w
	return f.archive
}
