package http

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

const testCorrelationID = "corr-portability-1"

var testPrincipal = identitydto.Principal{UserID: "user-1", WorkspaceID: "ws-1", SessionID: "session-1"}

func testNow() time.Time { return time.Date(2026, 8, 21, 9, 30, 0, 0, time.UTC) }

// allowAuth mirrors the session middleware: it injects a fixed principal and
// correlation id on the request context.
func allowAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := identitydto.WithPrincipal(r.Context(), testPrincipal)
		next.ServeHTTP(w, r.WithContext(observability.WithCorrelationID(ctx, testCorrelationID)))
	})
}

// noPrincipalAuth passes the request through without injecting a principal;
// the handlers' own guard must answer 401.
func noPrincipalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(observability.WithCorrelationID(r.Context(), testCorrelationID)))
	})
}

// Export-side fakes.

var errInstanceFailed = errors.New("instance lookup failed")

type fakeInstanceStore struct {
	id  string
	err error
}

func (f *fakeInstanceStore) InstanceID(context.Context) (string, error) { return f.id, f.err }

type fakeTodoExporter struct {
	records []dto.TodoExportRecord
	err     error
}

func (f *fakeTodoExporter) ExportTodos(_ context.Context, _, _ string, offset, _ int) ([]dto.TodoExportRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	if offset > 0 {
		return []dto.TodoExportRecord{}, nil
	}
	return f.records, nil
}

type fakeChannelExporter struct {
	records []dto.ChannelExportRecord
	err     error
}

func (f *fakeChannelExporter) ExportChannels(_ context.Context, _ ports.Principal) ([]dto.ChannelExportRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records, nil
}

type fakeDeliveryExporter struct {
	records []dto.DeliveryExportRecord
	err     error
}

func (f *fakeDeliveryExporter) ExportDeliveries(_ context.Context, _ string, offset, _ int) ([]dto.DeliveryExportRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	if offset > 0 {
		return []dto.DeliveryExportRecord{}, nil
	}
	return f.records, nil
}

// zipArchive is a test archive writer that streams a real zip into the
// response writer; it mirrors the production archive contract (sha256 per
// entry, the shared Files map filled, manifest.json appended last).
type zipArchive struct {
	zip    *zip.Writer
	hashes map[string]string
}

func (a *zipArchive) WriteEntry(_ context.Context, name string, encode func(context.Context, io.Writer) error) error {
	entry, err := a.zip.Create(name)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if err := encode(context.Background(), io.MultiWriter(entry, hasher)); err != nil {
		return err
	}
	a.hashes[name] = hex.EncodeToString(hasher.Sum(nil))
	return nil
}

func (a *zipArchive) WriteManifest(_ context.Context, manifest domain.Manifest) error {
	for name, checksum := range a.hashes {
		manifest.Files[name] = checksum
	}
	wire, err := json.Marshal(dto.NewBundleManifest(manifest))
	if err != nil {
		return err
	}
	entry, err := a.zip.Create(dto.ManifestEntry)
	if err != nil {
		return err
	}
	_, err = entry.Write(wire)
	return err
}

func (a *zipArchive) Close() error { return a.zip.Close() }

func zipArchiveFactory(w io.Writer) ports.ArchiveWriter {
	return &zipArchive{zip: zip.NewWriter(w), hashes: map[string]string{}}
}

// Import-side fakes.

// fakeBundleParser returns a fixed parse result or error and records every
// byte slice it was asked to parse.
type fakeBundleParser struct {
	parsed ports.ParsedBundle
	err    error
	calls  int
	got    []byte
}

func (f *fakeBundleParser) Parse(data []byte) (ports.ParsedBundle, error) {
	f.calls++
	f.got = data
	return f.parsed, f.err
}

// fakeImportStore keeps rows in memory keyed "workspaceID/importID"; Get
// reports domain.ErrImportNotFound for unknown rows.
type fakeImportStore struct {
	rows  map[string]dto.ImportRecordRow
	saved []dto.ImportRecordRow
}

func newFakeImportStore() *fakeImportStore {
	return &fakeImportStore{rows: map[string]dto.ImportRecordRow{}}
}

func (f *fakeImportStore) Save(_ context.Context, row dto.ImportRecordRow) error {
	f.saved = append(f.saved, row)
	f.rows[row.WorkspaceID+"/"+row.ID] = row
	return nil
}

func (f *fakeImportStore) Get(_ context.Context, workspaceID, importID string) (dto.ImportRecordRow, error) {
	row, ok := f.rows[workspaceID+"/"+importID]
	if !ok {
		return dto.ImportRecordRow{}, domain.ErrImportNotFound
	}
	return row, nil
}

func (f *fakeImportStore) Commit(_ context.Context, workspaceID, importID string, report dto.ImportReport, now time.Time) error {
	row := f.rows[workspaceID+"/"+importID]
	row.State = dto.ImportStateCommitted
	row.Report = &report
	row.CommittedAt = &now
	f.rows[workspaceID+"/"+importID] = row
	return nil
}

// fakeSourceRecordStore never has seen an imported record before.
type fakeSourceRecordStore struct{}

func (fakeSourceRecordStore) Fingerprints(context.Context, string, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (fakeSourceRecordStore) Targets(context.Context, string, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (fakeSourceRecordStore) Register(context.Context, dto.SourceRecord) error { return nil }

type fakeTodoImporter struct{ nextID int }

func (f *fakeTodoImporter) ImportTodo(context.Context, ports.Principal, dto.TodoImportRequest) (string, error) {
	f.nextID++
	return fmt.Sprintf("todo-%d", f.nextID), nil
}

type fakeChannelImporter struct{ nextID int }

func (f *fakeChannelImporter) ImportChannel(context.Context, ports.Principal, dto.ChannelImportRequest) (string, error) {
	f.nextID++
	return fmt.Sprintf("channel-%d", f.nextID), nil
}

type fakeDeliveryImporter struct{}

func (fakeDeliveryImporter) ImportDelivery(context.Context, ports.Principal, dto.DeliveryImportRequest) error {
	return nil
}

type fakeUnitOfWork struct{}

func (fakeUnitOfWork) Run(ctx context.Context, work func(context.Context) error) error {
	return work(ctx)
}

// Bundle record builders that satisfy the domain validators.

func bundleManifest() domain.Manifest {
	return domain.Manifest{
		SchemaVersion:    domain.SchemaVersion,
		SourceInstanceID: "instance-src",
		ExportedAt:       testNow().Add(-24 * time.Hour),
		Counts:           domain.ManifestCounts{Todos: 1, Deliveries: 1, Channels: 1},
		Files:            map[string]string{dto.TodosEntry: "hash"},
	}
}

func bundleTodo() domain.TodoRecord {
	created := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	return domain.TodoRecord{
		ID:        "todo-src-1",
		Title:     "buy milk",
		Status:    domain.TodoStatusPending,
		CreatedAt: created,
		UpdatedAt: created,
	}
}

func bundleChannel() domain.ChannelRecord {
	return domain.ChannelRecord{ID: "channel-src-1", Kind: domain.ChannelKindEmail, Address: "user@example.com", Enabled: true}
}

func bundleDelivery() domain.DeliveryRecord {
	when := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	return domain.DeliveryRecord{
		ID:                 "delivery-src-1",
		SourceTodoRecordID: "todo-src-1",
		Channel:            domain.ChannelKindEmail,
		State:              domain.DeliveryStateSucceeded,
		AttemptCount:       1,
		TodoTitleSnapshot:  "buy milk",
		ScheduledAt:        when,
		CreatedAt:          when,
		Origin:             domain.DeliveryOriginLocal,
	}
}

// Application-handler builders: the HTTP handler holds the concrete
// command/query handlers, wired here over the test fakes.

func newExportHandler(instance ports.InstanceIdentityStore, todos ports.TodoExporter, channels ports.ChannelExporter, deliveries ports.DeliveryExporter) *command.ExportBundleHandler {
	return &command.ExportBundleHandler{
		Instance:   instance,
		Todos:      todos,
		Channels:   channels,
		Deliveries: deliveries,
		Archive:    zipArchiveFactory,
		PageSize:   100,
		Now:        testNow,
	}
}

func happyExportHandler() *command.ExportBundleHandler {
	created := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	when := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	return newExportHandler(
		&fakeInstanceStore{id: "instance-1"},
		&fakeTodoExporter{records: []dto.TodoExportRecord{{
			ID: "todo-1", Title: "write report", Status: "pending", ReminderVersion: 1,
			CreatedAt: created, UpdatedAt: created,
		}}},
		&fakeChannelExporter{records: []dto.ChannelExportRecord{{
			ID: "channel-1", Kind: "email", Address: "user@example.com", Enabled: true,
		}}},
		&fakeDeliveryExporter{records: []dto.DeliveryExportRecord{{
			ID: "delivery-1", SourceTodoRecordID: "todo-1", Channel: "email", State: "succeeded",
			AttemptCount: 1, TodoTitleSnapshot: "write report", ScheduledAt: when, CreatedAt: when, Origin: "local",
		}}},
	)
}

func newUploadHandler(imports ports.ImportStore, parser *fakeBundleParser) *command.UploadImportHandler {
	return &command.UploadImportHandler{
		Imports:   imports,
		Sources:   fakeSourceRecordStore{},
		Parser:    parser,
		NewID:     func() string { return "import-1" },
		Now:       testNow,
		ImportTTL: 24 * time.Hour,
	}
}

func newConfirmHandler(imports ports.ImportStore, parser *fakeBundleParser) *command.ConfirmImportHandler {
	return &command.ConfirmImportHandler{
		Imports:    imports,
		Sources:    fakeSourceRecordStore{},
		Parser:     parser,
		Todos:      &fakeTodoImporter{},
		Channels:   &fakeChannelImporter{},
		Deliveries: fakeDeliveryImporter{},
		UoW:        fakeUnitOfWork{},
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:        testNow,
		ImportTTL:  24 * time.Hour,
	}
}

func newGetQuery(imports ports.ImportStore) *query.GetImportQuery {
	return &query.GetImportQuery{Imports: imports, Now: testNow, ImportTTL: 24 * time.Hour}
}

func newTestHandler(export *command.ExportBundleHandler, upload *command.UploadImportHandler, confirm *command.ConfirmImportHandler, get *query.GetImportQuery, maxBundleBytes int64) *Handler {
	return &Handler{
		Export:         export,
		Upload:         upload,
		Confirm:        confirm,
		Get:            get,
		MaxBundleBytes: maxBundleBytes,
	}
}

func serve(t *testing.T, h *Handler, auth func(http.Handler) http.Handler, method, target string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux, auth, h)
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func multipartBody(t *testing.T, field string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(field, "export.zip")
	if err != nil {
		t.Fatalf("create form file error = %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form file error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart error = %v", err)
	}
	return &buf, writer.FormDataContentType()
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode body error = %v, body = %s", err, recorder.Body.String())
	}
	return body
}

// assertEnvelope checks the stable error shape: status, code, a message, and
// the injected correlation id.
func assertEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, wantStatus, recorder.Body.String())
	}
	envelope := decodeBody(t, recorder)
	if envelope["code"] != wantCode {
		t.Fatalf("envelope = %#v, want code %q", envelope, wantCode)
	}
	if message, _ := envelope["message"].(string); message == "" {
		t.Fatalf("envelope missing message: %#v", envelope)
	}
	if envelope["correlationId"] != testCorrelationID {
		t.Fatalf("envelope correlationId = %#v, want %q", envelope["correlationId"], testCorrelationID)
	}
	return envelope
}

func TestExportStreamsZipWithAttachmentHeaders(t *testing.T) {
	export := happyExportHandler()
	handler := newTestHandler(export, nil, nil, nil, 1024)

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/export", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", contentType)
	}
	wantDisposition := `attachment; filename="artificial-brain-export-20260821.zip"`
	if disposition := recorder.Header().Get("Content-Disposition"); disposition != wantDisposition {
		t.Fatalf("Content-Disposition = %q, want %q", disposition, wantDisposition)
	}

	body := recorder.Body.Bytes()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response body is not a valid zip: %v", err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open entry %s error = %v", file.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read entry %s error = %v", file.Name, err)
		}
		entries[file.Name] = string(data)
	}
	for _, name := range []string{dto.TodosEntry, dto.DeliveriesEntry, dto.PreferencesEntry, dto.TodosCSVEntry, dto.ManifestEntry} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("zip missing entry %q, got %v", name, entries)
		}
	}
	if !strings.Contains(entries[dto.TodosEntry], `"id":"todo-1"`) {
		t.Fatalf("todos.json = %s, want todo-1", entries[dto.TodosEntry])
	}

	// The body is exactly what the handler streams into the writer.
	var direct bytes.Buffer
	if _, err := export.Handle(context.Background(), ports.Principal{UserID: "user-1", WorkspaceID: "ws-1"}, &direct); err != nil {
		t.Fatalf("direct Handle() error = %v", err)
	}
	if !bytes.Equal(body, direct.Bytes()) {
		t.Fatal("response body differs from the handler-streamed bytes")
	}
}

func TestExportBeforeStreamErrorMapsToEnvelope(t *testing.T) {
	export := newExportHandler(&fakeInstanceStore{err: errInstanceFailed}, &fakeTodoExporter{}, &fakeChannelExporter{}, &fakeDeliveryExporter{})
	handler := newTestHandler(export, nil, nil, nil, 1024)

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/export", nil, "")
	envelope := assertEnvelope(t, recorder, http.StatusInternalServerError, "internal_error")
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", recorder.Header().Get("Content-Type"))
	}
	_ = envelope
}

// A failure after streaming has started can no longer switch to a JSON
// envelope — the status line and headers are already flushed. The route
// leaves the partial body as-is; only the not-yet-started case maps.
func TestExportMidStreamFailureCannotSwitchToJSON(t *testing.T) {
	export := newExportHandler(&fakeInstanceStore{id: "instance-1"}, &fakeTodoExporter{err: errors.New("todo export failed")}, &fakeChannelExporter{}, &fakeDeliveryExporter{})
	handler := newTestHandler(export, nil, nil, nil, 1024)

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/export", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("mid-stream status = %d, want the already-flushed 200", recorder.Code)
	}
	body := recorder.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("mid-stream export wrote nothing")
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err == nil {
		if _, hasCode := envelope["code"]; hasCode {
			t.Fatalf("mid-stream failure sent a JSON envelope: %s", body)
		}
	}
}

func TestUploadReturns201WithImportIDAndPreview(t *testing.T) {
	parser := &fakeBundleParser{parsed: ports.ParsedBundle{
		Manifest: bundleManifest(),
		Todos:    []domain.TodoRecord{bundleTodo()},
		Channels: []domain.ChannelRecord{bundleChannel()},
	}}
	imports := newFakeImportStore()
	handler := newTestHandler(nil, newUploadHandler(imports, parser), nil, nil, 1024)

	uploaded := []byte("bundle-bytes")
	body, contentType := multipartBody(t, "bundle", uploaded)
	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports", body, contentType)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeBody(t, recorder)
	if response["importId"] != "import-1" {
		t.Fatalf("importId = %#v, want import-1", response["importId"])
	}
	preview, ok := response["preview"].(map[string]any)
	if !ok {
		t.Fatalf("preview missing or wrong shape: %#v", response)
	}
	if preview["new"] != float64(2) || preview["skipped"] != float64(0) {
		t.Fatalf("preview = %#v, want new=2 skipped=0", preview)
	}
	if details, ok := preview["details"].([]any); !ok || len(details) != 2 {
		t.Fatalf("preview details = %#v, want 2 lines", preview["details"])
	}
	if parser.calls != 1 || !bytes.Equal(parser.got, uploaded) {
		t.Fatalf("parser calls = %d, got = %q, want the uploaded bytes once", parser.calls, parser.got)
	}
	if len(imports.saved) != 1 {
		t.Fatalf("saved rows = %d, want 1", len(imports.saved))
	}
	row := imports.saved[0]
	if row.WorkspaceID != "ws-1" || row.State != dto.ImportStatePending || !bytes.Equal(row.Bundle, uploaded) || row.Preview == nil {
		t.Fatalf("saved row = %#v", row)
	}
}

func TestUploadMapsTypedParserErrorsToStableCodes(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"unsupported schema", fmt.Errorf("parse: %w", domain.ErrUnsupportedSchemaVersion), http.StatusUnprocessableEntity, "unsupported_schema_version"},
		{"checksum mismatch", fmt.Errorf("parse: %w", domain.ErrChecksumMismatch), http.StatusUnprocessableEntity, "checksum_mismatch"},
		{"bundle structure", fmt.Errorf("parse: %w", domain.ErrBundleStructure), http.StatusUnprocessableEntity, "bundle_invalid"},
		{"record invalid", fmt.Errorf("parse: %w", domain.ErrRecordInvalid), http.StatusUnprocessableEntity, "bundle_invalid"},
		{"manifest invalid", fmt.Errorf("parse: %w", domain.ErrManifestInvalid), http.StatusUnprocessableEntity, "bundle_invalid"},
		{"unknown parser error", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parser := &fakeBundleParser{err: testCase.err}
			handler := newTestHandler(nil, newUploadHandler(newFakeImportStore(), parser), nil, nil, 1024)
			body, contentType := multipartBody(t, "bundle", []byte("bundle-bytes"))
			recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports", body, contentType)
			assertEnvelope(t, recorder, testCase.wantStatus, testCase.wantCode)
		})
	}
}

func TestUploadRejectsOversizedBundleWithoutCallingParser(t *testing.T) {
	// Layer 2: the body fits the multipart guard but the part's bytes exceed
	// the cap; the LimitReader stops at MaxBundleBytes+1 without buffering
	// the excess.
	parser := &fakeBundleParser{}
	handler := newTestHandler(nil, newUploadHandler(newFakeImportStore(), parser), nil, nil, 16)
	body, contentType := multipartBody(t, "bundle", bytes.Repeat([]byte("a"), 64))
	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports", body, contentType)
	assertEnvelope(t, recorder, http.StatusUnprocessableEntity, "bundle_too_large")
	if parser.calls != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.calls)
	}

	// Layer 1: the body exceeds even the multipart slack; MaxBytesReader
	// aborts the parse before any part is read.
	parser = &fakeBundleParser{}
	handler = newTestHandler(nil, newUploadHandler(newFakeImportStore(), parser), nil, nil, 16)
	body, contentType = multipartBody(t, "bundle", bytes.Repeat([]byte("b"), 8192))
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports", body, contentType)
	assertEnvelope(t, recorder, http.StatusUnprocessableEntity, "bundle_too_large")
	if parser.calls != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.calls)
	}
}

func TestUploadRejectsBadMultipartAsBundleInvalid(t *testing.T) {
	parser := &fakeBundleParser{}
	handler := newTestHandler(nil, newUploadHandler(newFakeImportStore(), parser), nil, nil, 1024)

	// A multipart body without the bundle field.
	body, contentType := multipartBody(t, "other", []byte("bundle-bytes"))
	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports", body, contentType)
	assertEnvelope(t, recorder, http.StatusUnprocessableEntity, "bundle_invalid")

	// A non-multipart body altogether.
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports", strings.NewReader(`{}`), "application/json")
	assertEnvelope(t, recorder, http.StatusUnprocessableEntity, "bundle_invalid")

	if parser.calls != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.calls)
	}
}

func TestGetImportReturnsView(t *testing.T) {
	imports := newFakeImportStore()
	preview := dto.Preview{New: 1, Details: []dto.Decision{{Kind: domain.KindTodo, SourceRecordID: "todo-src-1", Outcome: string(domain.OutcomeNew)}}}
	imports.rows["ws-1/import-1"] = dto.ImportRecordRow{
		ID:               "import-1",
		WorkspaceID:      "ws-1",
		State:            dto.ImportStatePending,
		SourceInstanceID: "instance-src",
		Bundle:           []byte("bundle-bytes"),
		Preview:          &preview,
		CreatedAt:        testNow().Add(-time.Hour),
	}
	handler := newTestHandler(nil, nil, nil, newGetQuery(imports), 1024)

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/portability/imports/import-1", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	if body["importId"] != "import-1" || body["state"] != "pending" || body["sourceInstanceId"] != "instance-src" {
		t.Fatalf("body = %#v", body)
	}
	viewPreview, ok := body["preview"].(map[string]any)
	if !ok || viewPreview["new"] != float64(1) {
		t.Fatalf("preview = %#v", body["preview"])
	}
	if _, hasReport := body["report"]; hasReport {
		t.Fatalf("pending view carries no report: %#v", body)
	}
	if _, hasCreatedAt := body["createdAt"]; !hasCreatedAt {
		t.Fatalf("body missing createdAt: %#v", body)
	}
}

func TestGetImportMapsNotFoundTo404(t *testing.T) {
	handler := newTestHandler(nil, nil, nil, newGetQuery(newFakeImportStore()), 1024)
	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/portability/imports/missing", nil, "")
	assertEnvelope(t, recorder, http.StatusNotFound, "not_found")
}

func TestConfirmReturnsReportAndCommitsRow(t *testing.T) {
	imports := newFakeImportStore()
	imports.rows["ws-1/import-1"] = dto.ImportRecordRow{
		ID:               "import-1",
		WorkspaceID:      "ws-1",
		State:            dto.ImportStatePending,
		SourceInstanceID: "instance-src",
		Bundle:           []byte("stored-bytes"),
		CreatedAt:        testNow().Add(-time.Hour),
	}
	parser := &fakeBundleParser{parsed: ports.ParsedBundle{
		Manifest:   bundleManifest(),
		Todos:      []domain.TodoRecord{bundleTodo()},
		Channels:   []domain.ChannelRecord{bundleChannel()},
		Deliveries: []domain.DeliveryRecord{bundleDelivery()},
	}}
	handler := newTestHandler(nil, nil, newConfirmHandler(imports, parser), nil, 1024)

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports/import-1/confirm", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	report := decodeBody(t, recorder)
	if report["new"] != float64(3) || report["skipped"] != float64(0) || report["conflicts"] != float64(0) || report["invalid"] != float64(0) {
		t.Fatalf("report = %#v, want new=3", report)
	}
	if report["committedAt"] != testNow().UTC().Format(time.RFC3339) {
		t.Fatalf("committedAt = %#v, want %s", report["committedAt"], testNow().UTC().Format(time.RFC3339))
	}
	row := imports.rows["ws-1/import-1"]
	if row.State != dto.ImportStateCommitted || row.Report == nil || row.CommittedAt == nil {
		t.Fatalf("row after confirm = %#v", row)
	}
}

func TestConfirmMapsCommittedAndExpiredToImportConflict(t *testing.T) {
	imports := newFakeImportStore()
	imports.rows["ws-1/import-1"] = dto.ImportRecordRow{
		ID:          "import-1",
		WorkspaceID: "ws-1",
		State:       dto.ImportStateCommitted,
		CreatedAt:   testNow().Add(-time.Hour),
	}
	handler := newTestHandler(nil, nil, newConfirmHandler(imports, &fakeBundleParser{}), nil, 1024)

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports/import-1/confirm", nil, "")
	envelope := assertEnvelope(t, recorder, http.StatusConflict, "import_conflict")
	if !strings.Contains(envelope["message"].(string), "committed") {
		t.Fatalf("committed conflict message = %#v, want it to name the committed state", envelope["message"])
	}

	imports = newFakeImportStore()
	imports.rows["ws-1/import-1"] = dto.ImportRecordRow{
		ID:          "import-1",
		WorkspaceID: "ws-1",
		State:       dto.ImportStatePending,
		CreatedAt:   testNow().Add(-48 * time.Hour),
	}
	handler = newTestHandler(nil, nil, newConfirmHandler(imports, &fakeBundleParser{}), nil, 1024)
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports/import-1/confirm", nil, "")
	envelope = assertEnvelope(t, recorder, http.StatusConflict, "import_conflict")
	if !strings.Contains(envelope["message"].(string), "expired") {
		t.Fatalf("expired conflict message = %#v, want it to name the expired state", envelope["message"])
	}
}

func TestConfirmMapsNotFoundTo404(t *testing.T) {
	handler := newTestHandler(nil, nil, newConfirmHandler(newFakeImportStore(), &fakeBundleParser{}), nil, 1024)
	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/portability/imports/missing/confirm", nil, "")
	assertEnvelope(t, recorder, http.StatusNotFound, "not_found")
}

func TestRoutesGuardMissingPrincipalWith401(t *testing.T) {
	handler := newTestHandler(happyExportHandler(), newUploadHandler(newFakeImportStore(), &fakeBundleParser{}), newConfirmHandler(newFakeImportStore(), &fakeBundleParser{}), newGetQuery(newFakeImportStore()), 1024)
	body, contentType := multipartBody(t, "bundle", []byte("bundle-bytes"))
	routes := []struct {
		method      string
		target      string
		body        io.Reader
		contentType string
	}{
		{http.MethodPost, "/api/v1/portability/export", nil, ""},
		{http.MethodPost, "/api/v1/portability/imports", body, contentType},
		{http.MethodGet, "/api/v1/portability/imports/import-1", nil, ""},
		{http.MethodPost, "/api/v1/portability/imports/import-1/confirm", nil, ""},
	}
	for _, route := range routes {
		recorder := serve(t, handler, noPrincipalAuth, route.method, route.target, route.body, route.contentType)
		assertEnvelope(t, recorder, http.StatusUnauthorized, "unauthenticated")
	}
}
