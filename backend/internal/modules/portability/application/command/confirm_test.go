package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// confirmRig wires a ConfirmImportHandler over recording fakes; the shared
// call log captures importer invocations in order so tests can assert the
// channels → todos → deliveries execution sequence.
type confirmRig struct {
	imports    *fakeImportStore
	sources    *fakeSourceRecordStore
	parser     *fakeBundleParser
	todos      *fakeTodoImporter
	channels   *fakeChannelImporter
	deliveries *fakeDeliveryImporter
	uow        *fakeUnitOfWork
	logBuf     *bytes.Buffer
	callLog    []string
	handler    *ConfirmImportHandler
}

func newConfirmRig() *confirmRig {
	rig := &confirmRig{
		imports:    newFakeImportStore(),
		sources:    &fakeSourceRecordStore{fingerprints: map[string]string{}, targets: map[string]string{}},
		parser:     &fakeBundleParser{},
		todos:      &fakeTodoImporter{},
		channels:   &fakeChannelImporter{},
		deliveries: &fakeDeliveryImporter{},
		uow:        &fakeUnitOfWork{},
		logBuf:     &bytes.Buffer{},
	}
	rig.todos.log = &rig.callLog
	rig.channels.log = &rig.callLog
	rig.deliveries.log = &rig.callLog
	nextID := 0
	rig.handler = &ConfirmImportHandler{
		Imports:    rig.imports,
		Sources:    rig.sources,
		Parser:     rig.parser,
		Todos:      rig.todos,
		Channels:   rig.channels,
		Deliveries: rig.deliveries,
		UoW:        rig.uow,
		Log:        slog.New(slog.NewTextHandler(rig.logBuf, nil)),
		NewID:      func() string { nextID++; return fmt.Sprintf("confirm-%d", nextID) },
		Now:        importNow,
		ImportTTL:  24 * time.Hour,
	}
	return rig
}

// seedPendingImport stores a fresh pending row (created one hour ago, inside
// the 24h TTL) carrying the given bundle bytes.
func (r *confirmRig) seedPendingImport(bundle []byte) dto.ImportRecordRow {
	row := dto.ImportRecordRow{
		ID:               "import-1",
		WorkspaceID:      "ws-1",
		State:            dto.ImportStatePending,
		SourceInstanceID: "instance-src",
		Bundle:           bundle,
		CreatedAt:        importNow().Add(-time.Hour),
	}
	r.imports.rows[importRowKey(row.WorkspaceID, row.ID)] = row
	return row
}

// seedBundle wires the parser to return the manifest plus one record of each
// kind, with the delivery hanging off the todo. Source record ids are
// deliberately distinct from the importer fakes' generated target ids so the
// registration assertions can tell them apart.
func (r *confirmRig) seedBundle() (domain.ChannelRecord, domain.TodoRecord, domain.DeliveryRecord) {
	channel := validChannelRecord("channel-src-1")
	todo := validTodoRecord("todo-src-1", "buy milk")
	delivery := validDeliveryRecord("delivery-src-1", "todo-src-1")
	r.parser.parsed = ports.ParsedBundle{
		Manifest:   importManifest(),
		Todos:      []domain.TodoRecord{todo},
		Deliveries: []domain.DeliveryRecord{delivery},
		Channels:   []domain.ChannelRecord{channel},
	}
	return channel, todo, delivery
}

func TestConfirmImportHappyPathExecutesChannelsTodosDeliveriesInOrder(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	channel, todo, delivery := rig.seedBundle()

	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	// Execution order: channels first, then todos, then deliveries.
	wantOrder := []string{
		"channel:email:channel-src-1@example.com",
		"todo:buy milk",
		"delivery:delivery-src-1",
	}
	if !reflect.DeepEqual(rig.callLog, wantOrder) {
		t.Fatalf("call order = %v, want %v", rig.callLog, wantOrder)
	}

	// One source record per new row, with the record's fingerprint; the
	// delivery registers under its import idempotency key because the
	// reminder seam returns no row id.
	wantRegistered := []dto.SourceRecord{
		{WorkspaceID: "ws-1", SourceInstanceID: "instance-src", SourceRecordID: "channel-src-1", TargetKind: domain.KindChannel, TargetID: "channel-1", ContentFingerprint: domain.Fingerprint(channel)},
		{WorkspaceID: "ws-1", SourceInstanceID: "instance-src", SourceRecordID: "todo-src-1", TargetKind: domain.KindTodo, TargetID: "todo-1", ContentFingerprint: domain.Fingerprint(todo)},
		{WorkspaceID: "ws-1", SourceInstanceID: "instance-src", SourceRecordID: "delivery-src-1", TargetKind: domain.KindDelivery, TargetID: "import:instance-src:delivery-src-1", ContentFingerprint: domain.Fingerprint(delivery)},
	}
	if !reflect.DeepEqual(rig.sources.registered, wantRegistered) {
		t.Fatalf("registered = %+v, want %+v", rig.sources.registered, wantRegistered)
	}

	// The delivery resolves its todo reference to the id created this run and
	// carries the recorded history plus the source identity.
	if len(rig.deliveries.calls) != 1 {
		t.Fatalf("delivery imports = %d, want 1", len(rig.deliveries.calls))
	}
	gotDelivery := rig.deliveries.calls[0]
	if gotDelivery.TodoID != "todo-1" {
		t.Fatalf("delivery TodoID = %q, want the todo id created this run", gotDelivery.TodoID)
	}
	if gotDelivery.SourceInstanceID != "instance-src" || gotDelivery.SourceRecordID != "delivery-src-1" {
		t.Fatalf("delivery source identity = %q/%q, want instance-src/delivery-src-1", gotDelivery.SourceInstanceID, gotDelivery.SourceRecordID)
	}
	if gotDelivery.Channel != domain.ChannelKindEmail || gotDelivery.State != domain.DeliveryStateSucceeded {
		t.Fatalf("delivery history = %q/%q, want email/succeeded", gotDelivery.Channel, gotDelivery.State)
	}

	// The todo restore request mirrors the record with a fresh version
	// history (the bundle carries no optimistic-lock version).
	if len(rig.todos.calls) != 1 {
		t.Fatalf("todo imports = %d, want 1", len(rig.todos.calls))
	}
	if got := rig.todos.calls[0]; got.Title != "buy milk" || got.Status != domain.TodoStatusPending || got.Version != 0 || got.ReminderVersion != 1 {
		t.Fatalf("todo request = %+v, want mirrored record with Version 0", got)
	}

	// Report counts, committed-at, and the store commit all agree.
	if report.New != 3 || report.Skipped != 0 || report.Conflicts != 0 || report.Invalid != 0 {
		t.Fatalf("report counts = %+v, want 3 new", report)
	}
	if !report.CommittedAt.Equal(importNow()) {
		t.Fatalf("CommittedAt = %v, want %v", report.CommittedAt, importNow())
	}
	if len(rig.imports.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(rig.imports.commits))
	}
	commit := rig.imports.commits[0]
	if commit.workspaceID != "ws-1" || commit.importID != "import-1" {
		t.Fatalf("commit target = %q/%q, want ws-1/import-1", commit.workspaceID, commit.importID)
	}
	if !reflect.DeepEqual(commit.report, report) {
		t.Fatalf("committed report = %+v, want %+v", commit.report, report)
	}

	// The structured success event names the workspace, import, and counts.
	logLine := rig.logBuf.String()
	for _, want := range []string{"import committed", "workspaceId=ws-1", "importId=import-1", "new=3"} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log %q missing %q", logLine, want)
		}
	}
}

func TestConfirmImportReconfirmAfterCommitReportsConflictWithoutTouchingImporters(t *testing.T) {
	rig := newConfirmRig()
	row := rig.seedPendingImport([]byte("stored-bytes"))
	row.State = dto.ImportStateCommitted
	previousReport := dto.ImportReport{New: 3, CommittedAt: importNow().Add(-time.Minute)}
	row.Report = &previousReport
	rig.imports.rows[importRowKey(row.WorkspaceID, row.ID)] = row

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, domain.ErrImportConflict) {
		t.Fatalf("Handle() error = %v, want ErrImportConflict", err)
	}
	if rig.parser.calls != 0 || rig.uow.runs != 0 || len(rig.imports.commits) != 0 {
		t.Fatalf("re-confirm touched parser/uow/commit: %d/%d/%d", rig.parser.calls, rig.uow.runs, len(rig.imports.commits))
	}
	if len(rig.todos.calls) != 0 || len(rig.channels.calls) != 0 || len(rig.deliveries.calls) != 0 {
		t.Fatal("re-confirm touched the importers")
	}
}

func TestConfirmImportExpiredRowReportsExpired(t *testing.T) {
	rig := newConfirmRig()
	row := rig.seedPendingImport([]byte("stored-bytes"))
	row.CreatedAt = importNow().Add(-25 * time.Hour)
	rig.imports.rows[importRowKey(row.WorkspaceID, row.ID)] = row

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, domain.ErrImportExpired) {
		t.Fatalf("Handle() error = %v, want ErrImportExpired", err)
	}
	if rig.parser.calls != 0 || len(rig.todos.calls) != 0 {
		t.Fatal("expired confirm touched parser/importers")
	}
}

func TestConfirmImportAtExactTTLBoundaryStillConfirms(t *testing.T) {
	rig := newConfirmRig()
	row := rig.seedPendingImport([]byte("stored-bytes"))
	row.CreatedAt = importNow().Add(-24 * time.Hour)
	rig.imports.rows[importRowKey(row.WorkspaceID, row.ID)] = row
	rig.seedBundle()

	if _, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1"); err != nil {
		t.Fatalf("Handle() at the TTL boundary error = %v, want success", err)
	}
}

func TestConfirmImportUnknownImportReportsNotFound(t *testing.T) {
	rig := newConfirmRig()
	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "missing")
	if !errors.Is(err, domain.ErrImportNotFound) {
		t.Fatalf("Handle() error = %v, want ErrImportNotFound", err)
	}
}

func TestConfirmImportRedecidesFromStoredBytesNotStoredPreview(t *testing.T) {
	rig := newConfirmRig()
	row := rig.seedPendingImport([]byte("stored-bytes"))
	// A stale stored preview claiming everything is new must be ignored: the
	// fingerprint lookup at confirm time marks every record skipped.
	stale := dto.Preview{New: 3, Details: []dto.Decision{{Kind: domain.KindTodo, Outcome: string(domain.OutcomeNew)}}}
	row.Preview = &stale
	rig.imports.rows[importRowKey(row.WorkspaceID, row.ID)] = row
	channel, todo, delivery := rig.seedBundle()
	rig.sources.fingerprints = map[string]string{
		"instance-src:todo-src-1":     domain.Fingerprint(todo),
		"instance-src:channel-src-1":  domain.Fingerprint(channel),
		"instance-src:delivery-src-1": domain.Fingerprint(delivery),
	}

	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !bytes.Equal(rig.parser.gotData, []byte("stored-bytes")) {
		t.Fatalf("parser saw %q, want the stored bundle bytes", rig.parser.gotData)
	}
	if report.New != 0 || report.Skipped != 3 {
		t.Fatalf("report counts = %+v, want 3 skipped", report)
	}
	if len(rig.todos.calls) != 0 || len(rig.channels.calls) != 0 || len(rig.deliveries.calls) != 0 {
		t.Fatal("all-skipped confirm still executed importers")
	}
	if len(rig.sources.registered) != 0 {
		t.Fatalf("registered = %d, want 0 for skipped records", len(rig.sources.registered))
	}
	if len(rig.imports.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(rig.imports.commits))
	}
}

func TestConfirmImportImporterFailureFailsTheTransactionAndKeepsStatePending(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	rig.seedBundle()
	rig.todos.err = errTodoImport

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, errTodoImport) {
		t.Fatalf("Handle() error = %v, want %v", err, errTodoImport)
	}
	if rig.uow.runs != 1 {
		t.Fatalf("uow runs = %d, want 1", rig.uow.runs)
	}
	if len(rig.imports.commits) != 0 {
		t.Fatal("failed confirm still committed")
	}
	row := rig.imports.rows[importRowKey("ws-1", "import-1")]
	if row.State != dto.ImportStatePending {
		t.Fatalf("row state = %q, want pending after a failed confirm", row.State)
	}
	// Deliveries run last, so the todo failure keeps them untouched.
	if len(rig.deliveries.calls) != 0 {
		t.Fatal("deliveries executed despite the todo failure")
	}
}

func TestConfirmImportUnitOfWorkStartFailurePropagates(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	rig.seedBundle()
	rig.uow.err = errUnitOfWorkStart

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, errUnitOfWorkStart) {
		t.Fatalf("Handle() error = %v, want %v", err, errUnitOfWorkStart)
	}
	if len(rig.channels.calls) != 0 || len(rig.todos.calls) != 0 || len(rig.deliveries.calls) != 0 {
		t.Fatal("importers ran although the unit of work never started")
	}
	if len(rig.imports.commits) != 0 {
		t.Fatal("failed confirm still committed")
	}
}

func TestConfirmImportChannelExistsDowngradesToSkippedAndRegistersExistingTarget(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	channel, _, _ := rig.seedBundle()
	rig.channels.existsID = "channel-existing"

	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if report.New != 2 || report.Skipped != 1 {
		t.Fatalf("report counts = %+v, want 2 new / 1 skipped", report)
	}
	// The channel decision downgrades to skipped naming the reason; the todo
	// and delivery decisions stay new.
	var channelDecision *dto.Decision
	for i := range report.Details {
		if report.Details[i].Kind == domain.KindChannel {
			channelDecision = &report.Details[i]
		}
	}
	if channelDecision == nil || channelDecision.Outcome != string(domain.OutcomeSkipped) || channelDecision.Reason != "channel already exists" {
		t.Fatalf("channel decision = %+v, want skipped/channel already exists", channelDecision)
	}
	// The source record still lands, pointing at the existing channel id.
	want := dto.SourceRecord{WorkspaceID: "ws-1", SourceInstanceID: "instance-src", SourceRecordID: "channel-src-1", TargetKind: domain.KindChannel, TargetID: "channel-existing", ContentFingerprint: domain.Fingerprint(channel)}
	var gotChannelRecord *dto.SourceRecord
	for i := range rig.sources.registered {
		if rig.sources.registered[i].SourceRecordID == "channel-src-1" {
			gotChannelRecord = &rig.sources.registered[i]
		}
	}
	if gotChannelRecord == nil || !reflect.DeepEqual(*gotChannelRecord, want) {
		t.Fatalf("channel source record = %+v, want %+v", gotChannelRecord, want)
	}
	if len(rig.imports.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(rig.imports.commits))
	}
}

func TestConfirmImportChannelImporterFailureFailsTheWholeTransaction(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	rig.seedBundle()
	rig.channels.err = errChannelImport

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, errChannelImport) {
		t.Fatalf("Handle() error = %v, want %v", err, errChannelImport)
	}
	// Channels run first: nothing else executes and nothing commits.
	if len(rig.todos.calls) != 0 || len(rig.deliveries.calls) != 0 || len(rig.imports.commits) != 0 {
		t.Fatal("channel failure did not abort the rest of the transaction")
	}
}

func TestConfirmImportOrphanDeliveryBecomesInvalidWithoutFailingTheTransaction(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	orphan := validDeliveryRecord("delivery-orphan", "todo-missing")
	rig.parser.parsed = ports.ParsedBundle{
		Manifest:   importManifest(),
		Deliveries: []domain.DeliveryRecord{orphan},
	}

	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v, want the orphan delivery to not fail the confirm", err)
	}
	if report.Invalid != 1 || report.New != 0 {
		t.Fatalf("report counts = %+v, want 1 invalid", report)
	}
	if len(report.Details) != 1 || report.Details[0].Outcome != string(domain.OutcomeInvalid) || report.Details[0].Reason != "todo_not_found" {
		t.Fatalf("orphan decision = %+v, want invalid/todo_not_found", report.Details)
	}
	if len(rig.deliveries.calls) != 0 {
		t.Fatal("orphan delivery was executed")
	}
	if len(rig.sources.registered) != 0 {
		t.Fatal("orphan delivery registered a source record")
	}
	if len(rig.imports.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(rig.imports.commits))
	}
	// The todo reference was looked up in the source records.
	if len(rig.sources.targetIDSets) != 1 || !reflect.DeepEqual(rig.sources.targetIDSets[0], []string{"todo-missing"}) {
		t.Fatalf("Targets ids = %v, want [todo-missing]", rig.sources.targetIDSets)
	}
}

func TestConfirmImportDeliveryResolvesPreviouslyImportedTodoThroughSources(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	todo := validTodoRecord("todo-src-1", "buy milk")
	delivery := validDeliveryRecord("delivery-src-1", "todo-src-1")
	rig.parser.parsed = ports.ParsedBundle{
		Manifest:   importManifest(),
		Todos:      []domain.TodoRecord{todo},
		Deliveries: []domain.DeliveryRecord{delivery},
	}
	// The todo was imported by a previous run: fingerprint matches (skipped)
	// and its target id is resolvable.
	rig.sources.fingerprints = map[string]string{"instance-src:todo-src-1": domain.Fingerprint(todo)}
	rig.sources.targets = map[string]string{"instance-src:todo-src-1": "existing-todo-42"}

	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if report.New != 1 || report.Skipped != 1 {
		t.Fatalf("report counts = %+v, want 1 new / 1 skipped", report)
	}
	if len(rig.todos.calls) != 0 {
		t.Fatal("skipped todo was re-imported")
	}
	if len(rig.deliveries.calls) != 1 || rig.deliveries.calls[0].TodoID != "existing-todo-42" {
		t.Fatalf("delivery calls = %+v, want one import against existing-todo-42", rig.deliveries.calls)
	}
	// Only the new delivery registers; the skipped todo keeps its old record.
	if len(rig.sources.registered) != 1 || rig.sources.registered[0].SourceRecordID != "delivery-src-1" {
		t.Fatalf("registered = %+v, want only the delivery", rig.sources.registered)
	}
}

func TestConfirmImportDeliveryImporterFailureFailsTheTransaction(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	rig.seedBundle()
	rig.deliveries.failOnSourceRecordID = "delivery-src-1"

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, errDeliveryImport) {
		t.Fatalf("Handle() error = %v, want %v", err, errDeliveryImport)
	}
	if len(rig.imports.commits) != 0 {
		t.Fatal("failed confirm still committed")
	}
	if row := rig.imports.rows[importRowKey("ws-1", "import-1")]; row.State != dto.ImportStatePending {
		t.Fatalf("row state = %q, want pending", row.State)
	}
}

func TestConfirmImportCommitFailurePropagatesAndKeepsStatePending(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	rig.seedBundle()
	rig.imports.commitErr = errImportCommit

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, errImportCommit) {
		t.Fatalf("Handle() error = %v, want %v", err, errImportCommit)
	}
	if row := rig.imports.rows[importRowKey("ws-1", "import-1")]; row.State != dto.ImportStatePending {
		t.Fatalf("row state = %q, want pending after commit failure", row.State)
	}
}

func TestConfirmImportRegisterFailureFailsTheTransaction(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("stored-bytes"))
	rig.seedBundle()
	rig.sources.failRegisterOn = "todo-src-1"

	_, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if !errors.Is(err, domain.ErrSourceRecordExists) {
		t.Fatalf("Handle() error = %v, want ErrSourceRecordExists", err)
	}
	if len(rig.imports.commits) != 0 {
		t.Fatal("failed confirm still committed")
	}
}
