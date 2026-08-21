package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

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

// Import-side fakes (Task 9): parser, import store, source records, the three
// module importers, and the joinable unit of work.

var (
	errImportSave      = errors.New("import save failed")
	errImportCommit    = errors.New("import commit failed")
	errFingerprints    = errors.New("fingerprint lookup failed")
	errTargets         = errors.New("target lookup failed")
	errTodoImport      = errors.New("todo import failed")
	errChannelImport   = errors.New("channel import failed")
	errDeliveryImport  = errors.New("delivery import failed")
	errUnitOfWorkStart = errors.New("unit of work failed")
)

// fakeBundleParser returns a fixed parse result or error and records the
// bytes it was asked to parse.
type fakeBundleParser struct {
	parsed  ports.ParsedBundle
	err     error
	calls   int
	gotData []byte
}

func (f *fakeBundleParser) Parse(data []byte) (ports.ParsedBundle, error) {
	f.calls++
	f.gotData = data
	return f.parsed, f.err
}

// importCommitCall records one Commit invocation.
type importCommitCall struct {
	workspaceID string
	importID    string
	report      dto.ImportReport
	now         time.Time
}

// fakeImportStore keeps rows in memory keyed "workspaceID/importID". Get
// reports domain.ErrImportNotFound for unknown rows, mirroring the postgres
// adapter contract.
type fakeImportStore struct {
	rows      map[string]dto.ImportRecordRow
	saved     []dto.ImportRecordRow
	commits   []importCommitCall
	saveErr   error
	getErr    error
	commitErr error
}

func newFakeImportStore() *fakeImportStore {
	return &fakeImportStore{rows: map[string]dto.ImportRecordRow{}}
}

func importRowKey(workspaceID, importID string) string { return workspaceID + "/" + importID }

func (f *fakeImportStore) Save(_ context.Context, imp dto.ImportRecordRow) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, imp)
	f.rows[importRowKey(imp.WorkspaceID, imp.ID)] = imp
	return nil
}

func (f *fakeImportStore) Get(_ context.Context, workspaceID, importID string) (dto.ImportRecordRow, error) {
	if f.getErr != nil {
		return dto.ImportRecordRow{}, f.getErr
	}
	row, ok := f.rows[importRowKey(workspaceID, importID)]
	if !ok {
		return dto.ImportRecordRow{}, domain.ErrImportNotFound
	}
	return row, nil
}

func (f *fakeImportStore) Commit(_ context.Context, workspaceID, importID string, report dto.ImportReport, now time.Time) error {
	if f.commitErr != nil {
		return f.commitErr
	}
	f.commits = append(f.commits, importCommitCall{workspaceID: workspaceID, importID: importID, report: report, now: now})
	key := importRowKey(workspaceID, importID)
	row := f.rows[key]
	row.State = dto.ImportStateCommitted
	row.Report = &report
	committedAt := now
	row.CommittedAt = &committedAt
	f.rows[key] = row
	return nil
}

// fakeSourceRecordStore serves fixed fingerprint/target maps and records
// every registration in order.
type fakeSourceRecordStore struct {
	fingerprints      map[string]string
	targets           map[string]string
	fingerprintsErr   error
	targetsErr        error
	registerErr       error
	failRegisterOn    string // source record id whose registration reports ErrSourceRecordExists
	registered        []dto.SourceRecord
	gotInstanceID     string
	fingerprintIDSets [][]string
	targetIDSets      [][]string
}

func (f *fakeSourceRecordStore) Fingerprints(_ context.Context, sourceInstanceID string, ids []string) (map[string]string, error) {
	f.gotInstanceID = sourceInstanceID
	f.fingerprintIDSets = append(f.fingerprintIDSets, ids)
	if f.fingerprintsErr != nil {
		return nil, f.fingerprintsErr
	}
	return f.fingerprints, nil
}

func (f *fakeSourceRecordStore) Targets(_ context.Context, sourceInstanceID string, ids []string) (map[string]string, error) {
	f.gotInstanceID = sourceInstanceID
	f.targetIDSets = append(f.targetIDSets, ids)
	if f.targetsErr != nil {
		return nil, f.targetsErr
	}
	return f.targets, nil
}

func (f *fakeSourceRecordStore) Register(_ context.Context, record dto.SourceRecord) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	if f.failRegisterOn != "" && record.SourceRecordID == f.failRegisterOn {
		return domain.ErrSourceRecordExists
	}
	f.registered = append(f.registered, record)
	return nil
}

// fakeTodoImporter returns sequential ids ("todo-1", "todo-2", …), records
// every request, and appends "todo:<title>" to the shared call log when one
// is wired so tests can assert the channels → todos → deliveries order.
type fakeTodoImporter struct {
	err    error
	nextID int
	calls  []dto.TodoImportRequest
	log    *[]string
}

func (f *fakeTodoImporter) ImportTodo(_ context.Context, _ ports.Principal, record dto.TodoImportRequest) (string, error) {
	f.calls = append(f.calls, record)
	if f.log != nil {
		*f.log = append(*f.log, "todo:"+record.Title)
	}
	if f.err != nil {
		return "", f.err
	}
	f.nextID++
	return fmt.Sprintf("todo-%d", f.nextID), nil
}

// fakeChannelImporter mirrors fakeTodoImporter ("channel-1", …); a non-empty
// existsID makes every call return that id together with
// ports.ErrChannelExists, mirroring the cmd shim's translation of identity's
// duplicate-channel error.
type fakeChannelImporter struct {
	err      error
	existsID string
	nextID   int
	calls    []dto.ChannelImportRequest
	log      *[]string
}

func (f *fakeChannelImporter) ImportChannel(_ context.Context, _ ports.Principal, record dto.ChannelImportRequest) (string, error) {
	f.calls = append(f.calls, record)
	if f.log != nil {
		*f.log = append(*f.log, "channel:"+record.Kind+":"+record.Address)
	}
	if f.existsID != "" {
		return f.existsID, ports.ErrChannelExists
	}
	if f.err != nil {
		return "", f.err
	}
	f.nextID++
	return fmt.Sprintf("channel-%d", f.nextID), nil
}

// fakeDeliveryImporter records every request and appends
// "delivery:<sourceRecordId>" to the shared call log; failOnSourceRecordID
// fails the import of that one delivery.
type fakeDeliveryImporter struct {
	err                  error
	failOnSourceRecordID string
	calls                []dto.DeliveryImportRequest
	log                  *[]string
}

func (f *fakeDeliveryImporter) ImportDelivery(_ context.Context, _ ports.Principal, record dto.DeliveryImportRequest) error {
	f.calls = append(f.calls, record)
	if f.log != nil {
		*f.log = append(*f.log, "delivery:"+record.SourceRecordID)
	}
	if f.err != nil {
		return f.err
	}
	if f.failOnSourceRecordID != "" && record.SourceRecordID == f.failOnSourceRecordID {
		return errDeliveryImport
	}
	return nil
}

// fakeUnitOfWork runs the work function directly, surfacing its error the
// way a rolled-back transaction would; commitErr fails Run after the work
// itself succeeded.
type fakeUnitOfWork struct {
	runs      int
	err       error // returned instead of running the work
	commitErr error
}

func (f *fakeUnitOfWork) Run(ctx context.Context, work func(context.Context) error) error {
	f.runs++
	if f.err != nil {
		return f.err
	}
	if err := work(ctx); err != nil {
		return err
	}
	return f.commitErr
}
