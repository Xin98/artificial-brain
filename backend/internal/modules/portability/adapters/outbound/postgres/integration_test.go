package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "..", "..", "..", "..", "deploy", "migrations")
	if err := database.RunMigrations(ctx, url, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := database.OpenPool(ctx, url)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `truncate portability.portability_imports,
		portability.portability_source_records, public.instance_meta`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	return pool
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newImportRow builds a pending import row with the given bundle bytes and
// preview; report stays nil, as it does for every row before commit.
func newImportRow(workspaceID string, bundle []byte, preview *dto.Preview) dto.ImportRecordRow {
	return dto.ImportRecordRow{
		ID:               randomUUID(),
		WorkspaceID:      workspaceID,
		State:            dto.ImportStatePending,
		SourceInstanceID: "source-instance-" + workspaceID,
		Bundle:           bundle,
		Preview:          preview,
		CreatedAt:        testNow,
	}
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func TestImportStoreSaveGetRoundTrip(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewImportStore(pool)
	workspaceID := randomID(t)

	bundle := []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0x00, 0x01, 0xfe}
	preview := &dto.Preview{
		New:       2,
		Skipped:   1,
		Conflicts: 1,
		Invalid:   0,
		Details: []dto.Decision{
			{Kind: "todo", SourceRecordID: "rec-1", Outcome: "new", Reason: "first import"},
			{Kind: "delivery", SourceRecordID: "rec-2", Outcome: "conflict", Reason: "content changed"},
		},
		Truncated: true,
	}
	imp := newImportRow(workspaceID, bundle, preview)
	if err := store.Save(ctx, imp); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Get(ctx, workspaceID, imp.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != imp.ID || got.WorkspaceID != workspaceID || got.State != dto.ImportStatePending {
		t.Fatalf("Get() identity = %#v, want id %q workspace %q state pending", got, imp.ID, workspaceID)
	}
	if got.SourceInstanceID != imp.SourceInstanceID {
		t.Fatalf("Get() sourceInstanceID = %q, want %q", got.SourceInstanceID, imp.SourceInstanceID)
	}
	if !bytes.Equal(got.Bundle, bundle) {
		t.Fatalf("Get() bundle = %x, want %x", got.Bundle, bundle)
	}
	if !reflect.DeepEqual(got.Preview, preview) {
		t.Fatalf("Get() preview = %#v, want %#v", got.Preview, preview)
	}
	if got.Report != nil {
		t.Fatalf("Get() report = %#v, want nil before commit", got.Report)
	}
	if !got.CreatedAt.Equal(testNow) {
		t.Fatalf("Get() createdAt = %v, want %v", got.CreatedAt, testNow)
	}
	if got.CommittedAt != nil {
		t.Fatalf("Get() committedAt = %v, want nil before commit", got.CommittedAt)
	}

	// A pending row never carries a preview either when the upload stored none.
	plain := newImportRow(workspaceID, []byte("plain"), nil)
	if err := store.Save(ctx, plain); err != nil {
		t.Fatalf("Save(no preview) error = %v", err)
	}
	gotPlain, err := store.Get(ctx, workspaceID, plain.ID)
	if err != nil {
		t.Fatalf("Get(no preview) error = %v", err)
	}
	if gotPlain.Preview != nil || gotPlain.Report != nil {
		t.Fatalf("Get(no preview) = preview %#v report %#v, want both nil", gotPlain.Preview, gotPlain.Report)
	}
}

func TestImportStoreCommitFlipsStateAndRejectsSecondCommit(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewImportStore(pool)
	workspaceID := randomID(t)

	imp := newImportRow(workspaceID, []byte("bundle-bytes"), nil)
	if err := store.Save(ctx, imp); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	committedAt := testNow.Add(time.Hour)
	report := dto.ImportReport{
		New:         3,
		Skipped:     1,
		Conflicts:   0,
		Invalid:     2,
		Details:     []dto.Decision{{Kind: "todo", SourceRecordID: "rec-1", Outcome: "new", Reason: ""}},
		Truncated:   false,
		CommittedAt: committedAt,
	}
	if err := store.Commit(ctx, workspaceID, imp.ID, report, committedAt); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	var state string
	var storedReport, storedPreview []byte
	var storedCommittedAt time.Time
	if err := pool.QueryRow(ctx, `
		select state, report, preview, committed_at
		from portability.portability_imports where id = $1
	`, imp.ID).Scan(&state, &storedReport, &storedPreview, &storedCommittedAt); err != nil {
		t.Fatalf("raw row scan error = %v", err)
	}
	if state != dto.ImportStateCommitted {
		t.Fatalf("state = %q, want committed", state)
	}
	if !storedCommittedAt.Equal(committedAt) {
		t.Fatalf("committed_at = %v, want %v", storedCommittedAt, committedAt)
	}
	if storedReport == nil {
		t.Fatalf("report jsonb = NULL, want the committed report")
	}
	if storedPreview != nil {
		t.Fatalf("preview jsonb = %s, want NULL for a row saved without one", storedPreview)
	}

	got, err := store.Get(ctx, workspaceID, imp.ID)
	if err != nil {
		t.Fatalf("Get() after commit error = %v", err)
	}
	if got.State != dto.ImportStateCommitted {
		t.Fatalf("Get() state = %q, want committed", got.State)
	}
	if !reflect.DeepEqual(got.Report, &report) {
		t.Fatalf("Get() report = %#v, want %#v", got.Report, &report)
	}
	if got.CommittedAt == nil || !got.CommittedAt.Equal(committedAt) {
		t.Fatalf("Get() committedAt = %v, want %v", got.CommittedAt, committedAt)
	}

	// A second commit finds no pending row and reports the conflict.
	if err := store.Commit(ctx, workspaceID, imp.ID, report, committedAt.Add(time.Minute)); !errors.Is(err, domain.ErrImportConflict) {
		t.Fatalf("second Commit() error = %v, want ErrImportConflict", err)
	}
	// Committing an unknown id, or another workspace's row, is the same conflict.
	if err := store.Commit(ctx, workspaceID, randomID(t), report, committedAt); !errors.Is(err, domain.ErrImportConflict) {
		t.Fatalf("Commit(unknown id) error = %v, want ErrImportConflict", err)
	}
	if err := store.Commit(ctx, randomID(t), imp.ID, report, committedAt); !errors.Is(err, domain.ErrImportConflict) {
		t.Fatalf("Commit(wrong workspace) error = %v, want ErrImportConflict", err)
	}
}

func TestImportStoreGetScopesByWorkspace(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewImportStore(pool)
	workspaceID := randomID(t)

	imp := newImportRow(workspaceID, []byte("bundle"), nil)
	if err := store.Save(ctx, imp); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Another workspace never sees the row.
	if _, err := store.Get(ctx, randomID(t), imp.ID); !errors.Is(err, domain.ErrImportNotFound) {
		t.Fatalf("Get(wrong workspace) error = %v, want ErrImportNotFound", err)
	}
	// An unknown import id in the right workspace is also not found.
	if _, err := store.Get(ctx, workspaceID, randomID(t)); !errors.Is(err, domain.ErrImportNotFound) {
		t.Fatalf("Get(unknown id) error = %v, want ErrImportNotFound", err)
	}
}

func TestSourceRecordStoreRegisterAndLookups(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewSourceRecordStore(pool)
	workspaceID, instanceID := randomID(t), "source-instance-"+randomID(t)
	todoID, channelID := randomID(t), randomID(t)

	todoRecord := dto.SourceRecord{
		WorkspaceID:        workspaceID,
		SourceInstanceID:   instanceID,
		SourceRecordID:     "rec-todo",
		TargetKind:         "todo",
		TargetID:           todoID,
		ContentFingerprint: "fp-todo",
	}
	if err := store.Register(ctx, todoRecord); err != nil {
		t.Fatalf("Register(todo) error = %v", err)
	}
	channelRecord := dto.SourceRecord{
		WorkspaceID:        workspaceID,
		SourceInstanceID:   instanceID,
		SourceRecordID:     "rec-channel",
		TargetKind:         "channel",
		TargetID:           channelID,
		ContentFingerprint: "fp-channel",
	}
	if err := store.Register(ctx, channelRecord); err != nil {
		t.Fatalf("Register(channel) error = %v", err)
	}

	ids := []string{"rec-todo", "rec-channel", "rec-unknown"}
	fingerprints, err := store.Fingerprints(ctx, instanceID, ids)
	if err != nil {
		t.Fatalf("Fingerprints() error = %v", err)
	}
	wantFingerprints := map[string]string{
		instanceID + ":rec-todo":    "fp-todo",
		instanceID + ":rec-channel": "fp-channel",
	}
	if !reflect.DeepEqual(fingerprints, wantFingerprints) {
		t.Fatalf("Fingerprints() = %#v, want %#v (unknown ids absent)", fingerprints, wantFingerprints)
	}

	targets, err := store.Targets(ctx, instanceID, ids)
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	wantTargets := map[string]string{
		instanceID + ":rec-todo":    todoID,
		instanceID + ":rec-channel": channelID,
	}
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("Targets() = %#v, want %#v (unknown ids absent)", targets, wantTargets)
	}

	// Lookups are keyed by the source instance: another instance sees nothing.
	other, err := store.Fingerprints(ctx, "source-instance-other", ids)
	if err != nil {
		t.Fatalf("Fingerprints(other instance) error = %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("Fingerprints(other instance) = %#v, want empty", other)
	}

	// Re-registering the same (instance, record) pair reports the duplicate.
	duplicate := todoRecord
	duplicate.TargetID = randomID(t)
	if err := store.Register(ctx, duplicate); !errors.Is(err, domain.ErrSourceRecordExists) {
		t.Fatalf("duplicate Register() error = %v, want ErrSourceRecordExists", err)
	}
}

func TestMetaStoreInstanceIDCreatesOnce(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewMetaStore(pool)

	first, err := store.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID() error = %v", err)
	}
	if first == "" {
		t.Fatalf("InstanceID() = empty, want a generated uuid")
	}

	second, err := store.InstanceID(ctx)
	if err != nil {
		t.Fatalf("second InstanceID() error = %v", err)
	}
	if second != first {
		t.Fatalf("second InstanceID() = %q, want the stored %q", second, first)
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from public.instance_meta where key = 'instance_id'`).Scan(&rows); err != nil {
		t.Fatalf("instance_meta count error = %v", err)
	}
	if rows != 1 {
		t.Fatalf("instance_meta rows = %d, want exactly 1", rows)
	}
}

func TestMetaStoreInstanceIDConcurrentCallersShareOneID(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewMetaStore(pool)

	const callers = 2
	var wait sync.WaitGroup
	ids := make([]string, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			ids[index], errs[index] = store.InstanceID(ctx)
		}(i)
	}
	close(start)
	wait.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("InstanceID() caller %d error = %v", i, err)
		}
	}
	if ids[0] == "" || ids[1] != ids[0] {
		t.Fatalf("concurrent InstanceID() = %q and %q, want one shared id", ids[0], ids[1])
	}
	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from public.instance_meta where key = 'instance_id'`).Scan(&rows); err != nil {
		t.Fatalf("instance_meta count error = %v", err)
	}
	if rows != 1 {
		t.Fatalf("instance_meta rows after race = %d, want exactly 1", rows)
	}
}

func TestImportStoreJoinsAmbientTransaction(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewImportStore(pool)
	sources := NewSourceRecordStore(pool)
	runner := database.NewTxRunner(pool)
	workspaceID := randomID(t)

	// A failing unit of work rolls the import and source record back.
	failErr := errors.New("boom")
	err := runner.Run(ctx, func(txCtx context.Context) error {
		if err := store.Save(txCtx, newImportRow(workspaceID, []byte("bundle"), nil)); err != nil {
			return err
		}
		if err := sources.Register(txCtx, dto.SourceRecord{
			WorkspaceID:        workspaceID,
			SourceInstanceID:   "instance-uow",
			SourceRecordID:     "rec-uow",
			TargetKind:         "todo",
			TargetID:           randomID(t),
			ContentFingerprint: "fp-uow",
		}); err != nil {
			return err
		}
		return failErr
	})
	if !errors.Is(err, failErr) {
		t.Fatalf("Run() error = %v, want failErr", err)
	}
	var imports, records int
	if err := pool.QueryRow(ctx, `select count(*) from portability.portability_imports`).Scan(&imports); err != nil {
		t.Fatalf("import count error = %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from portability.portability_source_records`).Scan(&records); err != nil {
		t.Fatalf("source record count error = %v", err)
	}
	if imports != 0 || records != 0 {
		t.Fatalf("rows after rollback = %d imports, %d source records, want none", imports, records)
	}

	// A successful unit of work commits both writes atomically.
	imp := newImportRow(workspaceID, []byte("bundle"), nil)
	if err := runner.Run(ctx, func(txCtx context.Context) error {
		if err := store.Save(txCtx, imp); err != nil {
			return err
		}
		return store.Commit(txCtx, workspaceID, imp.ID, dto.ImportReport{CommittedAt: testNow}, testNow)
	}); err != nil {
		t.Fatalf("Run(commit) error = %v", err)
	}
	got, err := store.Get(ctx, workspaceID, imp.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != dto.ImportStateCommitted {
		t.Fatalf("Get() state = %q, want committed", got.State)
	}
}
