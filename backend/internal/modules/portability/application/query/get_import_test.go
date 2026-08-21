package query

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

var testNow = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

// fakeImportStore serves one fixed row or error for Get; it records every
// lookup and never mutates the stored row. Save and Commit complete the
// ports.ImportStore surface but are never exercised by the query.
type fakeImportStore struct {
	row   dto.ImportRecordRow
	err   error
	calls int
}

func (f *fakeImportStore) Save(_ context.Context, _ dto.ImportRecordRow) error { return nil }

func (f *fakeImportStore) Get(_ context.Context, workspaceID, importID string) (dto.ImportRecordRow, error) {
	f.calls++
	if f.err != nil {
		return dto.ImportRecordRow{}, f.err
	}
	if workspaceID != f.row.WorkspaceID || importID != f.row.ID {
		return dto.ImportRecordRow{}, domain.ErrImportNotFound
	}
	return f.row, nil
}

func (f *fakeImportStore) Commit(_ context.Context, _, _ string, _ dto.ImportReport, _ time.Time) error {
	return nil
}

func newQuery(store *fakeImportStore) *GetImportQuery {
	return &GetImportQuery{Imports: store, Now: func() time.Time { return testNow }, ImportTTL: 24 * time.Hour}
}

func pendingRow() dto.ImportRecordRow {
	preview := dto.Preview{
		New:     2,
		Skipped: 1,
		Details: []dto.Decision{
			{Kind: domain.KindTodo, SourceRecordID: "todo-1", Outcome: string(domain.OutcomeNew)},
		},
	}
	return dto.ImportRecordRow{
		ID:               "import-1",
		WorkspaceID:      "ws-1",
		State:            dto.ImportStatePending,
		SourceInstanceID: "instance-src",
		Bundle:           []byte("bundle-bytes"),
		Preview:          &preview,
		CreatedAt:        testNow.Add(-time.Hour),
	}
}

func TestGetImportReturnsPendingViewWithStoredPreview(t *testing.T) {
	row := pendingRow()
	view, err := newQuery(&fakeImportStore{row: row}).Handle(context.Background(), "ws-1", "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := dto.ImportView{
		ImportID:         "import-1",
		State:            dto.ImportStatePending,
		SourceInstanceID: "instance-src",
		Preview:          *row.Preview,
		CreatedAt:        row.CreatedAt,
	}
	if !reflect.DeepEqual(view, want) {
		t.Fatalf("view = %+v, want %+v", view, want)
	}
}

func TestGetImportReturnsCommittedViewWithReport(t *testing.T) {
	row := pendingRow()
	row.State = dto.ImportStateCommitted
	report := dto.ImportReport{New: 2, Skipped: 1, CommittedAt: testNow.Add(-30 * time.Minute)}
	committedAt := testNow.Add(-30 * time.Minute)
	row.Report = &report
	row.CommittedAt = &committedAt

	view, err := newQuery(&fakeImportStore{row: row}).Handle(context.Background(), "ws-1", "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if view.State != dto.ImportStateCommitted {
		t.Fatalf("state = %q, want committed", view.State)
	}
	if view.Report == nil || !reflect.DeepEqual(*view.Report, report) {
		t.Fatalf("report = %+v, want %+v", view.Report, report)
	}
	if view.CommittedAt == nil || !view.CommittedAt.Equal(committedAt) {
		t.Fatalf("committedAt = %v, want %v", view.CommittedAt, committedAt)
	}
}

func TestGetImportRendersExpiredPastTTLWithoutMutatingTheRow(t *testing.T) {
	row := pendingRow()
	row.CreatedAt = testNow.Add(-25 * time.Hour)
	store := &fakeImportStore{row: row}

	view, err := newQuery(store).Handle(context.Background(), "ws-1", "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if view.State != dto.ImportStateExpired {
		t.Fatalf("state = %q, want expired past the TTL", view.State)
	}
	// Lazy evaluation only: the stored row stays pending.
	if store.row.State != dto.ImportStatePending {
		t.Fatalf("stored row state = %q, want pending (no write-back)", store.row.State)
	}
}

func TestGetImportAtExactTTLBoundaryStaysPending(t *testing.T) {
	row := pendingRow()
	row.CreatedAt = testNow.Add(-24 * time.Hour)

	view, err := newQuery(&fakeImportStore{row: row}).Handle(context.Background(), "ws-1", "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if view.State != dto.ImportStatePending {
		t.Fatalf("state = %q, want pending exactly at the TTL boundary", view.State)
	}
}

func TestGetImportMissingPreviewRendersEmptyPreview(t *testing.T) {
	row := pendingRow()
	row.Preview = nil

	view, err := newQuery(&fakeImportStore{row: row}).Handle(context.Background(), "ws-1", "import-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(view.Preview, dto.Preview{}) {
		t.Fatalf("preview = %+v, want the zero preview", view.Preview)
	}
}

func TestGetImportNotFoundPropagates(t *testing.T) {
	_, err := newQuery(&fakeImportStore{row: pendingRow()}).Handle(context.Background(), "ws-other", "import-1")
	if !errors.Is(err, domain.ErrImportNotFound) {
		t.Fatalf("Handle() error = %v, want ErrImportNotFound", err)
	}
}
