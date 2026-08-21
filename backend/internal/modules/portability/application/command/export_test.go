package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

var exportedAt = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func testPrincipal() ports.Principal {
	return ports.Principal{UserID: "user-1", WorkspaceID: "ws-1"}
}

func strPtr(value string) *string        { return &value }
func timePtr(value time.Time) *time.Time { return &value }

// testTodos builds three full-history todos covering every status: pending
// with due date and timezone, completed, and deleted.
func testTodos() []dto.TodoExportRecord {
	created := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	return []dto.TodoExportRecord{
		{
			ID:              "todo-1",
			Title:           "buy milk",
			Description:     strPtr("oat milk"),
			DueAtUTC:        timePtr(created.Add(24 * time.Hour)),
			TimezoneAtInput: strPtr("Asia/Shanghai"),
			Status:          domain.TodoStatusPending,
			ReminderVersion: 1,
			CreatedAt:       created,
			UpdatedAt:       created.Add(time.Hour),
		},
		{
			ID:              "todo-2",
			Title:           "file taxes",
			Status:          domain.TodoStatusCompleted,
			ReminderVersion: 0,
			CreatedAt:       created,
			UpdatedAt:       created.Add(2 * time.Hour),
			CompletedAt:     timePtr(created.Add(2 * time.Hour)),
		},
		{
			ID:        "todo-3",
			Title:     "old draft",
			Status:    domain.TodoStatusDeleted,
			CreatedAt: created,
			UpdatedAt: created.Add(3 * time.Hour),
			DeletedAt: timePtr(created.Add(3 * time.Hour)),
		},
	}
}

// testDeliveries builds three deliveries referencing their source todo
// records across states and origins.
func testDeliveries() []dto.DeliveryExportRecord {
	scheduled := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	return []dto.DeliveryExportRecord{
		{
			ID:                 "delivery-1",
			SourceTodoRecordID: "todo-1",
			Channel:            domain.ChannelKindEmail,
			State:              domain.DeliveryStateSucceeded,
			AttemptCount:       1,
			ProviderMessageID:  strPtr("provider-message-1"),
			TodoTitleSnapshot:  "buy milk",
			ScheduledAt:        scheduled,
			CreatedAt:          scheduled,
			SubmittedAt:        timePtr(scheduled.Add(time.Minute)),
			FinalizedAt:        timePtr(scheduled.Add(time.Minute)),
			Origin:             domain.DeliveryOriginLocal,
		},
		{
			ID:                 "delivery-2",
			SourceTodoRecordID: "todo-2",
			Channel:            domain.ChannelKindSMS,
			State:              domain.DeliveryStateSuppressed,
			SuppressionReason:  strPtr("channel_disabled"),
			AttemptCount:       0,
			TodoTitleSnapshot:  "file taxes",
			ScheduledAt:        scheduled,
			CreatedAt:          scheduled,
			FinalizedAt:        timePtr(scheduled.Add(time.Minute)),
			Origin:             domain.DeliveryOriginLocal,
		},
		{
			ID:                 "delivery-3",
			SourceTodoRecordID: "todo-1",
			Channel:            domain.ChannelKindEmail,
			State:              domain.DeliveryStateFailed,
			AttemptCount:       3,
			LastErrorCode:      strPtr("smtp_timeout"),
			TodoTitleSnapshot:  "buy milk",
			ScheduledAt:        scheduled,
			CreatedAt:          scheduled,
			Origin:             domain.DeliveryOriginImported,
		},
	}
}

func testChannels() []dto.ChannelExportRecord {
	return []dto.ChannelExportRecord{
		{ID: "channel-1", Kind: domain.ChannelKindEmail, Address: "user@example.com", Enabled: true},
		{ID: "channel-2", Kind: domain.ChannelKindSMS, Address: "+8613800137001", Enabled: false},
	}
}

// exportFixture wires a handler whose paged exporters return two pages each
// with the given page size.
type exportFixture struct {
	handler    *ExportBundleHandler
	instance   *fakeInstanceStore
	todos      *fakeTodoExporter
	deliveries *fakeDeliveryExporter
	channels   *fakeChannelExporter
	factory    *fakeArchiveFactory
	archive    *fakeArchive
	out        *bytes.Buffer
}

func newExportFixture(pageSize int) *exportFixture {
	todos := testTodos()
	deliveries := testDeliveries()
	archive := newFakeArchive()
	factory := &fakeArchiveFactory{archive: archive}
	fixture := &exportFixture{
		instance:   &fakeInstanceStore{id: "instance-1"},
		todos:      &fakeTodoExporter{pages: [][]dto.TodoExportRecord{todos[:2], todos[2:]}, failAt: -1},
		deliveries: &fakeDeliveryExporter{pages: [][]dto.DeliveryExportRecord{deliveries[:2], deliveries[2:]}, failAt: -1},
		channels:   &fakeChannelExporter{records: testChannels()},
		factory:    factory,
		archive:    archive,
		out:        &bytes.Buffer{},
	}
	fixture.handler = &ExportBundleHandler{
		Instance:   fixture.instance,
		Todos:      fixture.todos,
		Channels:   fixture.channels,
		Deliveries: fixture.deliveries,
		Archive:    factory.newArchive,
		PageSize:   pageSize,
		Now:        func() time.Time { return exportedAt },
	}
	return fixture
}

func wantEntryOrder() []string {
	return []string{dto.TodosEntry, dto.DeliveriesEntry, dto.PreferencesEntry, dto.TodosCSVEntry, dto.ManifestEntry}
}

// assertHashesMatchEntryBytes pins that every manifest hash equals the sha256
// of the bytes the archive actually streamed, and that manifest.json itself
// is never listed.
func assertHashesMatchEntryBytes(t *testing.T, fixture *exportFixture, manifest domain.Manifest) {
	t.Helper()
	if _, ok := manifest.Files[dto.ManifestEntry]; ok {
		t.Fatalf("manifest files list itself: %v", manifest.Files)
	}
	if len(manifest.Files) != len(wantEntryOrder())-1 {
		t.Fatalf("manifest files = %v, want one hash per data entry", manifest.Files)
	}
	for name, checksum := range manifest.Files {
		data, ok := fixture.archive.data[name]
		if !ok {
			t.Fatalf("manifest lists %q but no entry was streamed", name)
		}
		sum := sha256.Sum256(data)
		if checksum != hex.EncodeToString(sum[:]) {
			t.Fatalf("manifest hash for %q = %q, want sha256 of streamed bytes", name, checksum)
		}
	}
}

func TestExportBundleStreamsEntriesInContractOrder(t *testing.T) {
	fixture := newExportFixture(2)

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if !reflect.DeepEqual(fixture.archive.entries, wantEntryOrder()) {
		t.Fatalf("entry order = %v, want %v", fixture.archive.entries, wantEntryOrder())
	}
	if fixture.factory.gotWriter != fixture.out {
		t.Fatalf("archive factory writer = %v, want the output writer", fixture.factory.gotWriter)
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on success")
	}
	if manifest.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("manifest schema version = %q, want %q", manifest.SchemaVersion, domain.SchemaVersion)
	}
	if manifest.SourceInstanceID != "instance-1" {
		t.Fatalf("manifest source instance = %q, want instance-1", manifest.SourceInstanceID)
	}
	if !manifest.ExportedAt.Equal(exportedAt) {
		t.Fatalf("manifest exported at = %v, want %v", manifest.ExportedAt, exportedAt)
	}
	wantCounts := domain.ManifestCounts{Todos: 3, Deliveries: 3, Channels: 2}
	if manifest.Counts != wantCounts {
		t.Fatalf("manifest counts = %+v, want %+v", manifest.Counts, wantCounts)
	}
	if err := domain.ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	assertHashesMatchEntryBytes(t, fixture, manifest)
}

func TestExportBundleStreamsTodosJSONRecords(t *testing.T) {
	fixture := newExportFixture(2)

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if manifest.Counts.Todos != 3 {
		t.Fatalf("todo count = %d, want 3", manifest.Counts.Todos)
	}

	var records []dto.TodoExportRecord
	if err := json.Unmarshal(fixture.archive.data[dto.TodosEntry], &records); err != nil {
		t.Fatalf("todos.json does not parse: %v", err)
	}
	want := testTodos()
	if len(records) != len(want) {
		t.Fatalf("todos.json records = %d, want %d", len(records), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(records[i], want[i]) {
			t.Fatalf("todos.json record %d = %#v, want %#v", i, records[i], want[i])
		}
	}
}

func TestExportBundleDeliveriesCarrySourceTodoRecordID(t *testing.T) {
	fixture := newExportFixture(2)

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if manifest.Counts.Deliveries != 3 {
		t.Fatalf("delivery count = %d, want 3", manifest.Counts.Deliveries)
	}

	raw := string(fixture.archive.data[dto.DeliveriesEntry])
	if !strings.Contains(raw, `"sourceTodoRecordId":"todo-1"`) {
		t.Fatalf("reminder-deliveries.json missing sourceTodoRecordId wire key: %s", raw)
	}
	var records []dto.DeliveryExportRecord
	if err := json.Unmarshal(fixture.archive.data[dto.DeliveriesEntry], &records); err != nil {
		t.Fatalf("reminder-deliveries.json does not parse: %v", err)
	}
	want := testDeliveries()
	if len(records) != len(want) {
		t.Fatalf("delivery records = %d, want %d", len(records), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(records[i], want[i]) {
			t.Fatalf("delivery record %d = %#v, want %#v", i, records[i], want[i])
		}
	}
}

func TestExportBundleStreamsPreferences(t *testing.T) {
	fixture := newExportFixture(2)

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if manifest.Counts.Channels != 2 {
		t.Fatalf("channel count = %d, want 2", manifest.Counts.Channels)
	}
	if fixture.channels.gotPrincipal != testPrincipal() {
		t.Fatalf("channel exporter principal = %#v, want %#v", fixture.channels.gotPrincipal, testPrincipal())
	}
	var records []dto.ChannelExportRecord
	if err := json.Unmarshal(fixture.archive.data[dto.PreferencesEntry], &records); err != nil {
		t.Fatalf("preferences.json does not parse: %v", err)
	}
	if !reflect.DeepEqual(records, testChannels()) {
		t.Fatalf("preferences.json = %#v, want %#v", records, testChannels())
	}
}

func TestExportBundleWritesHumanReadableCSV(t *testing.T) {
	fixture := newExportFixture(2)

	if _, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(fixture.archive.data[dto.TodosCSVEntry]))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("todos.csv does not parse: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("todos.csv rows = %d, want header + 3 records", len(rows))
	}
	if !reflect.DeepEqual(rows[0], dto.TodosCSVHeader) {
		t.Fatalf("todos.csv header = %v, want %v", rows[0], dto.TodosCSVHeader)
	}
	wantFirst := []string{"todo-1", "buy milk", "pending", "2026-08-02T09:00:00Z", "Asia/Shanghai", "2026-08-01T09:00:00Z", "", ""}
	if !reflect.DeepEqual(rows[1], wantFirst) {
		t.Fatalf("todos.csv first row = %v, want %v", rows[1], wantFirst)
	}
	wantThird := []string{"todo-3", "old draft", "deleted", "", "", "2026-08-01T09:00:00Z", "", "2026-08-01T12:00:00Z"}
	if !reflect.DeepEqual(rows[3], wantThird) {
		t.Fatalf("todos.csv deleted row = %v, want %v", rows[3], wantThird)
	}
}

func TestExportBundlePagesUntilShortPage(t *testing.T) {
	fixture := newExportFixture(2)

	if _, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	wantJSONPass := []pageCall{
		{workspaceID: "ws-1", userID: "user-1", offset: 0, limit: 2},
		{workspaceID: "ws-1", userID: "user-1", offset: 2, limit: 2},
	}
	// todos.csv re-streams the same source, so the todo exporter pages the
	// full history twice: once for todos.json, once for the CSV copy.
	if len(fixture.todos.calls) != 4 {
		t.Fatalf("todo exporter calls = %d, want 4 (json + csv passes)", len(fixture.todos.calls))
	}
	if !reflect.DeepEqual(fixture.todos.calls[:2], wantJSONPass) {
		t.Fatalf("todo exporter json pass = %#v, want %#v", fixture.todos.calls[:2], wantJSONPass)
	}
	if !reflect.DeepEqual(fixture.todos.calls[2:], wantJSONPass) {
		t.Fatalf("todo exporter csv pass = %#v, want the same paging as the json pass", fixture.todos.calls[2:])
	}
	wantDeliveryCalls := []pageCall{
		{workspaceID: "ws-1", offset: 0, limit: 2},
		{workspaceID: "ws-1", offset: 2, limit: 2},
	}
	if !reflect.DeepEqual(fixture.deliveries.calls, wantDeliveryCalls) {
		t.Fatalf("delivery exporter calls = %#v, want %#v", fixture.deliveries.calls, wantDeliveryCalls)
	}
}

func TestExportBundlePagesPastExactMultiple(t *testing.T) {
	fixture := newExportFixture(3)
	// Exactly one full page: the handler must fetch once more and stop at the
	// empty page.
	fixture.todos.pages = [][]dto.TodoExportRecord{testTodos()}

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if manifest.Counts.Todos != 3 {
		t.Fatalf("todo count = %d, want 3", manifest.Counts.Todos)
	}
	if len(fixture.todos.calls) != 4 {
		t.Fatalf("todo exporter calls = %d, want 4 (full page + empty page, json + csv)", len(fixture.todos.calls))
	}
	for _, call := range fixture.todos.calls {
		if call.limit != 3 {
			t.Fatalf("todo exporter limit = %d, want 3", call.limit)
		}
	}
}

func TestExportBundleDefaultsPageSize(t *testing.T) {
	for name, pageSize := range map[string]int{"zero": 0, "negative": -5} {
		t.Run(name, func(t *testing.T) {
			fixture := newExportFixture(pageSize)

			if _, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if len(fixture.todos.calls) == 0 {
				t.Fatalf("todo exporter never called")
			}
			if call := fixture.todos.calls[0]; call.limit != 200 {
				t.Fatalf("todo exporter limit = %d, want default 200", call.limit)
			}
		})
	}
}

func TestExportBundleEmptyWorkspace(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.todos.pages = nil
	fixture.deliveries.pages = nil
	fixture.channels.records = nil

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	for _, entry := range []string{dto.TodosEntry, dto.DeliveriesEntry, dto.PreferencesEntry} {
		if got := string(fixture.archive.data[entry]); got != "[]" {
			t.Fatalf("%s = %q, want empty JSON array", entry, got)
		}
	}
	if got := string(fixture.archive.data[dto.TodosCSVEntry]); got != strings.Join(dto.TodosCSVHeader, ",")+"\n" {
		t.Fatalf("todos.csv = %q, want header row only", got)
	}
	if manifest.Counts != (domain.ManifestCounts{}) {
		t.Fatalf("manifest counts = %+v, want zero counts", manifest.Counts)
	}
	if err := domain.ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	assertHashesMatchEntryBytes(t, fixture, manifest)
}

func TestExportBundleManifestEntryCarriesWireShape(t *testing.T) {
	fixture := newExportFixture(2)

	if _, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	var wire map[string]json.RawMessage
	if err := json.Unmarshal(fixture.archive.data[dto.ManifestEntry], &wire); err != nil {
		t.Fatalf("manifest.json does not parse: %v", err)
	}
	for _, key := range []string{"schemaVersion", "sourceInstanceId", "exportedAt", "counts", "files"} {
		if _, ok := wire[key]; !ok {
			t.Fatalf("manifest.json missing key %q: %s", key, fixture.archive.data[dto.ManifestEntry])
		}
	}
	var counts map[string]int
	if err := json.Unmarshal(wire["counts"], &counts); err != nil {
		t.Fatalf("manifest counts do not parse: %v", err)
	}
	if counts["todos"] != 3 || counts["deliveries"] != 3 || counts["channels"] != 2 {
		t.Fatalf("manifest counts wire = %v, want todos 3 deliveries 3 channels 2", counts)
	}
}

func TestExportBundleTodoExporterErrorAborts(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.todos.failAt = 1 // the second page fails mid-stream

	manifest, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errTodosFailed) {
		t.Fatalf("Handle() error = %v, want errTodosFailed", err)
	}
	if !reflect.DeepEqual(manifest, domain.Manifest{}) {
		t.Fatalf("Handle() manifest = %+v, want zero manifest", manifest)
	}
	if fixture.archive.manifestDone || len(fixture.archive.entries) > 0 {
		t.Fatalf("manifest written despite exporter failure: entries=%v", fixture.archive.entries)
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on the error path")
	}
}

func TestExportBundleDeliveryExporterErrorAborts(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.deliveries.failAt = 0

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errDeliveriesFailed) {
		t.Fatalf("Handle() error = %v, want errDeliveriesFailed", err)
	}
	if fixture.archive.manifestDone {
		t.Fatalf("manifest written despite delivery exporter failure")
	}
	if !reflect.DeepEqual(fixture.archive.entries, []string{dto.TodosEntry}) {
		t.Fatalf("entries = %v, want only todos.json before the failure", fixture.archive.entries)
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on the error path")
	}
}

func TestExportBundleChannelExporterErrorAborts(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.channels.err = errChannelsFailed

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errChannelsFailed) {
		t.Fatalf("Handle() error = %v, want errChannelsFailed", err)
	}
	if fixture.archive.manifestDone {
		t.Fatalf("manifest written despite channel exporter failure")
	}
	if !reflect.DeepEqual(fixture.archive.entries, []string{dto.TodosEntry, dto.DeliveriesEntry}) {
		t.Fatalf("entries = %v, want the two paged entries before the failure", fixture.archive.entries)
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on the error path")
	}
}

func TestExportBundleInstanceErrorAbortsBeforeArchive(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.instance.err = errInstanceFailed

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errInstanceFailed) {
		t.Fatalf("Handle() error = %v, want errInstanceFailed", err)
	}
	if fixture.factory.calls != 0 {
		t.Fatalf("archive factory called %d times, want none before instance id resolution", fixture.factory.calls)
	}
	if fixture.archive.closed {
		t.Fatalf("archive closed although it was never created")
	}
}

func TestExportBundleArchiveEntryErrorAborts(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.archive.entryErr = errArchiveEntry

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errArchiveEntry) {
		t.Fatalf("Handle() error = %v, want errArchiveEntry", err)
	}
	if fixture.archive.manifestDone {
		t.Fatalf("manifest written despite archive entry failure")
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on the error path")
	}
}

func TestExportBundleArchiveManifestErrorAborts(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.archive.manifestErr = errArchiveManifest

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errArchiveManifest) {
		t.Fatalf("Handle() error = %v, want errArchiveManifest", err)
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on the error path")
	}
}

func TestExportBundleCloseErrorPropagatesOnSuccess(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.archive.closeErr = errArchiveClose

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errArchiveClose) {
		t.Fatalf("Handle() error = %v, want errArchiveClose", err)
	}
}

func TestExportBundleCloseErrorDoesNotMaskStreamingError(t *testing.T) {
	fixture := newExportFixture(2)
	fixture.todos.failAt = 0
	fixture.archive.closeErr = errArchiveClose

	_, err := fixture.handler.Handle(context.Background(), testPrincipal(), fixture.out)
	if !errors.Is(err, errTodosFailed) {
		t.Fatalf("Handle() error = %v, want the streaming error to win", err)
	}
	if !fixture.archive.closed {
		t.Fatalf("archive not closed on the error path")
	}
}
