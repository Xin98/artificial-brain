package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// Import-flow fixtures: a fixed clock, a source instance, and record builders
// that satisfy the domain validators.

func importNow() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }

func importManifest() domain.Manifest {
	return domain.Manifest{
		SchemaVersion:    domain.SchemaVersion,
		SourceInstanceID: "instance-src",
		ExportedAt:       time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC),
		Counts:           domain.ManifestCounts{Todos: 1, Deliveries: 1, Channels: 1},
		Files:            map[string]string{dto.TodosEntry: "hash"},
	}
}

func validTodoRecord(id, title string) domain.TodoRecord {
	created := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	return domain.TodoRecord{
		ID:              id,
		Title:           title,
		Description:     strPtr("imported " + title),
		DueAtUTC:        timePtr(created.Add(24 * time.Hour)),
		TimezoneAtInput: strPtr("Asia/Shanghai"),
		Status:          domain.TodoStatusPending,
		ReminderVersion: 1,
		CreatedAt:       created,
		UpdatedAt:       created.Add(time.Hour),
	}
}

func validChannelRecord(id string) domain.ChannelRecord {
	return domain.ChannelRecord{ID: id, Kind: domain.ChannelKindEmail, Address: id + "@example.com", Enabled: true}
}

func validDeliveryRecord(id, sourceTodoRecordID string) domain.DeliveryRecord {
	when := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	return domain.DeliveryRecord{
		ID:                 id,
		SourceTodoRecordID: sourceTodoRecordID,
		Channel:            domain.ChannelKindEmail,
		State:              domain.DeliveryStateSucceeded,
		AttemptCount:       1,
		TodoTitleSnapshot:  "snapshot",
		ScheduledAt:        when,
		CreatedAt:          when,
		Origin:             domain.DeliveryOriginLocal,
	}
}

// newUploadHandler wires the handler over the given fakes with a counting id
// source and the fixed clock.
func newUploadHandler(imports *fakeImportStore, sources *fakeSourceRecordStore, parser *fakeBundleParser) *UploadImportHandler {
	nextID := 0
	return &UploadImportHandler{
		Imports:   imports,
		Sources:   sources,
		Parser:    parser,
		NewID:     func() string { nextID++; return fmt.Sprintf("import-%d", nextID) },
		Now:       importNow,
		ImportTTL: 24 * time.Hour,
	}
}

func TestUploadImportHappyPathDecidesPerKindAgainstExistingFingerprints(t *testing.T) {
	todoSkipped := validTodoRecord("todo-1", "already imported")
	todoNew := validTodoRecord("todo-2", "fresh todo")
	channel := validChannelRecord("channel-1")
	delivery := validDeliveryRecord("delivery-1", "todo-1")

	parser := &fakeBundleParser{parsed: ports.ParsedBundle{
		Manifest:   importManifest(),
		Todos:      []domain.TodoRecord{todoSkipped, todoNew},
		Deliveries: []domain.DeliveryRecord{delivery},
		Channels:   []domain.ChannelRecord{channel},
	}}
	sources := &fakeSourceRecordStore{fingerprints: map[string]string{
		// todo-1 unchanged ⇒ skipped; channel-1 changed ⇒ conflict.
		"instance-src:todo-1":    domain.Fingerprint(todoSkipped),
		"instance-src:channel-1": "stale-fingerprint",
	}}
	imports := newFakeImportStore()
	handler := newUploadHandler(imports, sources, parser)

	bundle := []byte("bundle-bytes")
	importID, preview, err := handler.Handle(context.Background(), testPrincipal(), bundle)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if importID != "import-1" {
		t.Fatalf("importID = %q, want import-1", importID)
	}

	wantPreview := dto.Preview{
		New:       2,
		Skipped:   1,
		Conflicts: 1,
		Invalid:   0,
		Details: []dto.Decision{
			{Kind: domain.KindTodo, SourceRecordID: "todo-1", Outcome: string(domain.OutcomeSkipped), Reason: "fingerprint unchanged since last import"},
			{Kind: domain.KindTodo, SourceRecordID: "todo-2", Outcome: string(domain.OutcomeNew)},
			{Kind: domain.KindChannel, SourceRecordID: "channel-1", Outcome: string(domain.OutcomeConflict), Reason: "fingerprint changed since last import"},
			{Kind: domain.KindDelivery, SourceRecordID: "delivery-1", Outcome: string(domain.OutcomeNew)},
		},
	}
	if !reflect.DeepEqual(preview, wantPreview) {
		t.Fatalf("preview = %+v, want %+v", preview, wantPreview)
	}

	// Every record id is fingerprint-checked against the manifest's instance.
	if len(sources.fingerprintIDSets) != 1 {
		t.Fatalf("Fingerprints calls = %d, want 1", len(sources.fingerprintIDSets))
	}
	if sources.gotInstanceID != "instance-src" {
		t.Fatalf("Fingerprints instance = %q, want instance-src", sources.gotInstanceID)
	}
	gotIDs := append([]string(nil), sources.fingerprintIDSets[0]...)
	wantIDs := []string{"todo-1", "todo-2", "channel-1", "delivery-1"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("Fingerprints ids = %v, want %v", gotIDs, wantIDs)
	}

	// The row lands pending with the uploaded bytes verbatim and the preview
	// stored for GetImport.
	if len(imports.saved) != 1 {
		t.Fatalf("saved rows = %d, want 1", len(imports.saved))
	}
	row := imports.saved[0]
	if row.ID != importID || row.WorkspaceID != "ws-1" {
		t.Fatalf("row identity = %q/%q, want import-1/ws-1", row.ID, row.WorkspaceID)
	}
	if row.State != dto.ImportStatePending {
		t.Fatalf("row state = %q, want pending", row.State)
	}
	if row.SourceInstanceID != "instance-src" {
		t.Fatalf("row source instance = %q, want instance-src", row.SourceInstanceID)
	}
	if !bytes.Equal(row.Bundle, bundle) {
		t.Fatalf("row bundle = %q, want the uploaded bytes", row.Bundle)
	}
	if row.Preview == nil || !reflect.DeepEqual(*row.Preview, wantPreview) {
		t.Fatalf("stored preview = %+v, want %+v", row.Preview, wantPreview)
	}
	if !row.CreatedAt.Equal(importNow()) {
		t.Fatalf("row createdAt = %v, want %v", row.CreatedAt, importNow())
	}
	if row.Report != nil || row.CommittedAt != nil {
		t.Fatalf("pending row carries report/committedAt: %+v", row)
	}
}

func TestUploadImportMarksInvalidRecordsAndKeepsThemOutOfDecide(t *testing.T) {
	invalidTodo := domain.TodoRecord{ID: "todo-bad", Title: "bad status", Status: "archived"}
	invalidChannel := domain.ChannelRecord{ID: "channel-bad", Kind: "pigeon", Address: "x"}
	invalidDelivery := domain.DeliveryRecord{ID: "delivery-bad", SourceTodoRecordID: "", Channel: "email", State: "succeeded", Origin: "local"}
	validTodo := validTodoRecord("todo-ok", "fine")

	parser := &fakeBundleParser{parsed: ports.ParsedBundle{
		Manifest:   importManifest(),
		Todos:      []domain.TodoRecord{invalidTodo, validTodo},
		Deliveries: []domain.DeliveryRecord{invalidDelivery},
		Channels:   []domain.ChannelRecord{invalidChannel},
	}}
	sources := &fakeSourceRecordStore{fingerprints: map[string]string{}}
	imports := newFakeImportStore()
	handler := newUploadHandler(imports, sources, parser)

	_, preview, err := handler.Handle(context.Background(), testPrincipal(), []byte("bundle"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if preview.New != 1 || preview.Invalid != 3 || preview.Skipped != 0 || preview.Conflicts != 0 {
		t.Fatalf("preview counts = %+v, want 1 new / 3 invalid", preview)
	}
	// Valid decisions come first (Decide order), invalid decisions appended.
	if len(preview.Details) != 4 {
		t.Fatalf("details = %d, want 4", len(preview.Details))
	}
	if preview.Details[0].Outcome != string(domain.OutcomeNew) || preview.Details[0].SourceRecordID != "todo-ok" {
		t.Fatalf("first decision = %+v, want new todo-ok", preview.Details[0])
	}
	for i, want := range []struct{ kind, id string }{
		{domain.KindTodo, "todo-bad"},
		{domain.KindChannel, "channel-bad"},
		{domain.KindDelivery, "delivery-bad"},
	} {
		got := preview.Details[i+1]
		if got.Kind != want.kind || got.SourceRecordID != want.id || got.Outcome != string(domain.OutcomeInvalid) {
			t.Fatalf("decision[%d] = %+v, want invalid %s/%s", i+1, got, want.kind, want.id)
		}
		// The reason surfaces the validation error text, naming the record.
		if !strings.Contains(got.Reason, want.id) {
			t.Fatalf("decision[%d] reason %q does not name record %s", i+1, got.Reason, want.id)
		}
	}
	// Every record id — valid and invalid alike — is fingerprint-checked.
	if got := sources.fingerprintIDSets[0]; len(got) != 4 {
		t.Fatalf("Fingerprints ids = %v, want all four record ids", got)
	}
}

func TestUploadImportParserTypedErrorsPropagateAndStoreNothing(t *testing.T) {
	for _, parseErr := range []error{
		domain.ErrBundleStructure,
		domain.ErrChecksumMismatch,
		domain.ErrUnsupportedSchemaVersion,
		domain.ErrRecordInvalid,
	} {
		parser := &fakeBundleParser{err: parseErr}
		imports := newFakeImportStore()
		sources := &fakeSourceRecordStore{}
		handler := newUploadHandler(imports, sources, parser)

		importID, _, err := handler.Handle(context.Background(), testPrincipal(), []byte("bundle"))
		if !errors.Is(err, parseErr) {
			t.Fatalf("Handle() error = %v, want %v", err, parseErr)
		}
		if importID != "" {
			t.Fatalf("importID = %q, want empty on parse failure", importID)
		}
		if len(imports.saved) != 0 {
			t.Fatalf("saved rows = %d, want 0 after parse failure", len(imports.saved))
		}
		if len(sources.fingerprintIDSets) != 0 {
			t.Fatalf("Fingerprints called %d times, want 0 after parse failure", len(sources.fingerprintIDSets))
		}
	}
}

func TestUploadImportZeroRecordBundleStillStoresEmptyPreview(t *testing.T) {
	parser := &fakeBundleParser{parsed: ports.ParsedBundle{
		Manifest: importManifest(),
	}}
	imports := newFakeImportStore()
	sources := &fakeSourceRecordStore{fingerprints: map[string]string{}}
	handler := newUploadHandler(imports, sources, parser)

	importID, preview, err := handler.Handle(context.Background(), testPrincipal(), []byte("empty-bundle"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if importID == "" {
		t.Fatal("importID is empty, want a stored import")
	}
	if preview.New != 0 || preview.Skipped != 0 || preview.Conflicts != 0 || preview.Invalid != 0 || preview.Truncated {
		t.Fatalf("preview = %+v, want all-zero empty preview", preview)
	}
	if len(preview.Details) != 0 {
		t.Fatalf("details = %d, want 0", len(preview.Details))
	}
	if len(imports.saved) != 1 {
		t.Fatalf("saved rows = %d, want 1", len(imports.saved))
	}
}

func TestUploadImportTruncatesDetailsAtHundredPerOutcome(t *testing.T) {
	todos := make([]domain.TodoRecord, 0, 150)
	for i := 0; i < 150; i++ {
		todos = append(todos, validTodoRecord("todo-"+string(rune('A'+i/26))+string(rune('a'+i%26)), "title"))
	}
	parser := &fakeBundleParser{parsed: ports.ParsedBundle{Manifest: importManifest(), Todos: todos}}
	imports := newFakeImportStore()
	sources := &fakeSourceRecordStore{fingerprints: map[string]string{}}
	handler := newUploadHandler(imports, sources, parser)

	_, preview, err := handler.Handle(context.Background(), testPrincipal(), []byte("big-bundle"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if preview.New != 150 {
		t.Fatalf("new = %d, want 150", preview.New)
	}
	if !preview.Truncated {
		t.Fatal("Truncated = false, want true past the 100-per-outcome cap")
	}
	if len(preview.Details) != 100 {
		t.Fatalf("details = %d, want 100", len(preview.Details))
	}
	if preview.Details[0].SourceRecordID != todos[0].ID {
		t.Fatalf("first detail = %+v, want the first record", preview.Details[0])
	}
}

func TestUploadImportStoreFailurePropagates(t *testing.T) {
	parser := &fakeBundleParser{parsed: ports.ParsedBundle{Manifest: importManifest(), Todos: []domain.TodoRecord{validTodoRecord("t", "x")}}}
	imports := newFakeImportStore()
	imports.saveErr = errImportSave
	sources := &fakeSourceRecordStore{fingerprints: map[string]string{}}
	handler := newUploadHandler(imports, sources, parser)

	importID, _, err := handler.Handle(context.Background(), testPrincipal(), []byte("bundle"))
	if !errors.Is(err, errImportSave) {
		t.Fatalf("Handle() error = %v, want %v", err, errImportSave)
	}
	if importID != "" {
		t.Fatalf("importID = %q, want empty on save failure", importID)
	}
}

func TestUploadImportFingerprintLookupFailurePropagates(t *testing.T) {
	parser := &fakeBundleParser{parsed: ports.ParsedBundle{Manifest: importManifest(), Todos: []domain.TodoRecord{validTodoRecord("t", "x")}}}
	imports := newFakeImportStore()
	sources := &fakeSourceRecordStore{fingerprintsErr: errFingerprints}
	handler := newUploadHandler(imports, sources, parser)

	_, _, err := handler.Handle(context.Background(), testPrincipal(), []byte("bundle"))
	if !errors.Is(err, errFingerprints) {
		t.Fatalf("Handle() error = %v, want %v", err, errFingerprints)
	}
	if len(imports.saved) != 0 {
		t.Fatalf("saved rows = %d, want 0 after fingerprint failure", len(imports.saved))
	}
}
