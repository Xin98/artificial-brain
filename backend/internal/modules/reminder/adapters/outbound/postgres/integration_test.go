package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

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
	if _, err := pool.Exec(ctx, `truncate reminder.reminder_plans cascade`); err != nil {
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

func newPlan(t *testing.T, workspaceID, todoID string, reminderVersion int) domain.ReminderPlan {
	t.Helper()
	plan, err := domain.NewReminderPlan(randomID(t), workspaceID, todoID, reminderVersion,
		testNow.Add(time.Duration(reminderVersion)*time.Hour), []string{"sms"}, testNow)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	return plan
}

func statusesByTodo(t *testing.T, pool *pgxpool.Pool, workspaceID, todoID string) map[int]string {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		select todo_reminder_version, status from reminder.reminder_plans
		where workspace_id = $1 and todo_id = $2
		order by todo_reminder_version
	`, workspaceID, todoID)
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	defer rows.Close()
	statuses := map[int]string{}
	for rows.Next() {
		var version int
		var status string
		if err := rows.Scan(&version, &status); err != nil {
			t.Fatalf("scan error = %v", err)
		}
		statuses[version] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error = %v", err)
	}
	return statuses
}

func TestPlanStoreSaveAndDuplicateIdempotency(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewPlanStore(pool)
	workspaceID, todoID := randomID(t), randomID(t)

	plan := newPlan(t, workspaceID, todoID, 1)
	if err := store.Save(ctx, plan); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	duplicate := newPlan(t, workspaceID, todoID, 1)
	if err := store.Save(ctx, duplicate); !errors.Is(err, domain.ErrPlanExists) {
		t.Fatalf("duplicate Save() error = %v, want ErrPlanExists", err)
	}

	nextVersion := newPlan(t, workspaceID, todoID, 2)
	if err := store.Save(ctx, nextVersion); err != nil {
		t.Fatalf("Save(next version) error = %v", err)
	}

	otherTodo := newPlan(t, workspaceID, randomID(t), 1)
	if err := store.Save(ctx, otherTodo); err != nil {
		t.Fatalf("Save(other todo) error = %v", err)
	}
}

func TestPlanStoreGetScopesByWorkspace(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewPlanStore(pool)
	workspaceID, todoID := randomID(t), randomID(t)

	plan := newPlan(t, workspaceID, todoID, 1)
	if err := store.Save(ctx, plan); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Found: every persisted field round-trips.
	got, err := store.Get(ctx, workspaceID, plan.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != plan.ID || got.WorkspaceID != plan.WorkspaceID || got.TodoID != plan.TodoID {
		t.Fatalf("Get() identity = %#v, want %#v", got, plan)
	}
	if got.TodoReminderVersion != plan.TodoReminderVersion || !got.ScheduledAtUTC.Equal(plan.ScheduledAtUTC) {
		t.Fatalf("Get() schedule = %#v, want %#v", got, plan)
	}
	if len(got.RequestedChannels) != len(plan.RequestedChannels) || got.RequestedChannels[0] != plan.RequestedChannels[0] {
		t.Fatalf("Get() channels = %#v, want %#v", got.RequestedChannels, plan.RequestedChannels)
	}
	if got.Status != plan.Status || !got.CreatedAt.Equal(plan.CreatedAt) || got.RevokedAt != nil {
		t.Fatalf("Get() status/created/revoked = %#v, want %#v", got, plan)
	}

	// Not found: an unknown plan id maps to ErrPlanNotFound.
	if _, err := store.Get(ctx, workspaceID, randomID(t)); !errors.Is(err, domain.ErrPlanNotFound) {
		t.Fatalf("Get(unknown id) error = %v, want ErrPlanNotFound", err)
	}

	// Wrong workspace: the same plan id in another workspace is not visible.
	if _, err := store.Get(ctx, randomID(t), plan.ID); !errors.Is(err, domain.ErrPlanNotFound) {
		t.Fatalf("Get(wrong workspace) error = %v, want ErrPlanNotFound", err)
	}
}

func TestPlanStoreRevokePlannedCutoff(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewPlanStore(pool)
	workspaceID, todoID := randomID(t), randomID(t)

	for version := 1; version <= 3; version++ {
		if err := store.Save(ctx, newPlan(t, workspaceID, todoID, version)); err != nil {
			t.Fatalf("Save(v%d) error = %v", version, err)
		}
	}

	revokeAt := testNow.Add(30 * time.Minute)
	if err := store.RevokePlanned(ctx, workspaceID, todoID, 2, revokeAt); err != nil {
		t.Fatalf("RevokePlanned() error = %v", err)
	}

	statuses := statusesByTodo(t, pool, workspaceID, todoID)
	if statuses[1] != "revoked" || statuses[2] != "revoked" {
		t.Fatalf("statuses below cutoff = %#v, want revoked", statuses)
	}
	if statuses[3] != "planned" {
		t.Fatalf("status above cutoff = %q, want planned", statuses[3])
	}

	var revokedAt time.Time
	if err := pool.QueryRow(ctx, `
		select revoked_at from reminder.reminder_plans
		where workspace_id = $1 and todo_id = $2 and todo_reminder_version = 1
	`, workspaceID, todoID).Scan(&revokedAt); err != nil {
		t.Fatalf("revoked_at scan error = %v", err)
	}
	if !revokedAt.Equal(revokeAt) {
		t.Fatalf("revoked_at = %v, want %v", revokedAt, revokeAt)
	}

	// Revoking again is a no-op: the v3 plan above the cutoff stays planned.
	if err := store.RevokePlanned(ctx, workspaceID, todoID, 2, revokeAt.Add(time.Minute)); err != nil {
		t.Fatalf("second RevokePlanned() error = %v", err)
	}
	statuses = statusesByTodo(t, pool, workspaceID, todoID)
	if statuses[3] != "planned" {
		t.Fatalf("status above cutoff after re-revoke = %q, want planned", statuses[3])
	}
}

func strPtr(value string) *string        { return &value }
func timePtr(value time.Time) *time.Time { return &value }

// newImportedDeliveryFixture rebuilds a historical delivery without a plan,
// as the import seam restores one.
func newImportedDeliveryFixture(t *testing.T, workspaceID, ownerUserID, todoID string, reminderVersion int, key string, createdAt time.Time) domain.ReminderDelivery {
	t.Helper()
	submitted := createdAt.Add(time.Minute)
	finalized := submitted
	delivery, err := domain.RestoreDelivery(randomID(t), workspaceID, ownerUserID, todoID, reminderVersion,
		"email", "review the launch checklist", key, domain.StateSucceeded,
		nil, 1, strPtr("provider-message-"+key), nil,
		createdAt.Add(-time.Hour), createdAt, &submitted, &finalized,
		nil, nil, nil)
	if err != nil {
		t.Fatalf("RestoreDelivery() error = %v", err)
	}
	return delivery
}

// seedExportDelivery saves one local delivery for a fresh todo with an
// explicit created_at so export ordering is deterministic.
func seedExportDelivery(t *testing.T, ctx context.Context, plans *PlanStore, deliveries *DeliveryStore, workspaceID string, createdAt time.Time) domain.ReminderDelivery {
	t.Helper()
	todoID := randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	delivery := newDeliveryFixture(t, workspaceID, randomID(t), todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	delivery.CreatedAt = createdAt
	if err := deliveries.Save(ctx, delivery); err != nil {
		t.Fatalf("Save(export fixture) error = %v", err)
	}
	return delivery
}

func TestDeliveryStoreSaveImportedInsertsNullPlanAndImportedOrigin(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	key := "import:" + randomID(t) + ":" + randomID(t)

	delivery := newImportedDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, key, testNow)
	if err := store.SaveImported(ctx, delivery); err != nil {
		t.Fatalf("SaveImported() error = %v", err)
	}

	// The raw row carries a NULL plan_id and the imported origin.
	var planID *string
	var origin string
	if err := pool.QueryRow(ctx, `
		select plan_id, origin from reminder.reminder_deliveries where idempotency_key = $1
	`, key).Scan(&planID, &origin); err != nil {
		t.Fatalf("raw row scan error = %v", err)
	}
	if planID != nil {
		t.Fatalf("plan_id = %v, want NULL for an imported delivery", *planID)
	}
	if origin != "imported" {
		t.Fatalf("origin = %q, want imported", origin)
	}

	// Every persisted field round-trips through Export, plan included as empty.
	rows, err := store.Export(ctx, workspaceID, 0, 50)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Export() rows = %d, want 1", len(rows))
	}
	if !reflect.DeepEqual(rows[0], delivery) {
		t.Fatalf("Export() row = %#v, want %#v", rows[0], delivery)
	}

	// The historical 22-column reads also tolerate the NULL plan: List backs
	// the reminders listing, which must keep serving after an import inserts
	// plan-less rows (the empty PlanID stays empty).
	listed, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Limit: 50})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List() rows = %d, want 1", len(listed))
	}
	if listed[0].ID != delivery.ID || listed[0].PlanID != "" {
		t.Fatalf("List() row = %#v, want id %s with an empty plan id", listed[0], delivery.ID)
	}

	// Re-importing the same key maps to ErrDeliveryExists.
	duplicate := newImportedDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, key, testNow)
	if err := store.SaveImported(ctx, duplicate); !errors.Is(err, domain.ErrDeliveryExists) {
		t.Fatalf("duplicate SaveImported() error = %v, want ErrDeliveryExists", err)
	}
}

// TestDeliveryStoreTodoChannelUniqueAppliesToLocalRowsOnly pins migration
// 008's partial unique index: the bundle wire shape carries no reminder
// version, so every delivery of a rescheduled todo restores as
// (todo, 0, channel) — imported history must not collide on that triple,
// while local planning rows keep the fallback uniqueness.
func TestDeliveryStoreTodoChannelUniqueAppliesToLocalRowsOnly(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)

	// Two imported rows on the same (todo, 0, channel) with distinct import
	// keys both insert — no unique violation.
	first := newImportedDeliveryFixture(t, workspaceID, ownerUserID, todoID, 0, "import:inst-x:rec-1", testNow)
	if err := store.SaveImported(ctx, first); err != nil {
		t.Fatalf("SaveImported(first) error = %v, want imported history exempt from the todo/channel uniqueness", err)
	}
	second := newImportedDeliveryFixture(t, workspaceID, ownerUserID, todoID, 0, "import:inst-x:rec-2", testNow.Add(time.Minute))
	if err := store.SaveImported(ctx, second); err != nil {
		t.Fatalf("SaveImported(second) error = %v, want imported history exempt from the todo/channel uniqueness", err)
	}
	// The import idempotency key still guards re-imports of the same record.
	repeat := newImportedDeliveryFixture(t, workspaceID, ownerUserID, todoID, 0, "import:inst-x:rec-1", testNow)
	if err := store.SaveImported(ctx, repeat); !errors.Is(err, domain.ErrDeliveryExists) {
		t.Fatalf("SaveImported(repeat key) error = %v, want ErrDeliveryExists", err)
	}

	// Two local rows on the same (todo, version, channel) still collide, even
	// with distinct idempotency keys — the collision comes from the partial
	// unique index, not the key constraint.
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	local := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "sms", testNow.Add(time.Hour))
	if err := store.Save(ctx, local); err != nil {
		t.Fatalf("Save(local) error = %v", err)
	}
	collision := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "sms", testNow.Add(2*time.Hour))
	collision.IdempotencyKey = "distinct:" + randomID(t)
	if err := store.Save(ctx, collision); !errors.Is(err, domain.ErrDeliveryExists) {
		t.Fatalf("Save(local collision) error = %v, want ErrDeliveryExists from the local-only todo/channel uniqueness", err)
	}
}

func TestDeliveryStoreExportOrdersByCreatedAtWithOrigin(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, otherWorkspaceID := randomID(t), randomID(t)

	imported1 := newImportedDeliveryFixture(t, workspaceID, randomID(t), randomID(t), 1, "import:inst-a:rec-1", testNow.Add(1*time.Minute))
	if err := store.SaveImported(ctx, imported1); err != nil {
		t.Fatalf("SaveImported(first) error = %v", err)
	}
	local := seedExportDelivery(t, ctx, plans, store, workspaceID, testNow.Add(2*time.Minute))
	imported2 := newImportedDeliveryFixture(t, workspaceID, randomID(t), randomID(t), 1, "import:inst-a:rec-2", testNow.Add(3*time.Minute))
	if err := store.SaveImported(ctx, imported2); err != nil {
		t.Fatalf("SaveImported(second) error = %v", err)
	}
	// Another workspace's delivery must never appear in this export.
	other := newImportedDeliveryFixture(t, otherWorkspaceID, randomID(t), randomID(t), 1, "import:inst-b:rec-1", testNow.Add(4*time.Minute))
	if err := store.SaveImported(ctx, other); err != nil {
		t.Fatalf("SaveImported(other workspace) error = %v", err)
	}

	// All states and both origins export, ordered by created_at.
	rows, err := store.Export(ctx, workspaceID, 0, 50)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("Export() rows = %d, want 3", len(rows))
	}
	if rows[0].ID != imported1.ID || rows[1].ID != local.ID || rows[2].ID != imported2.ID {
		t.Fatalf("Export() order = [%s %s %s], want created_at order", rows[0].ID, rows[1].ID, rows[2].ID)
	}
	if rows[0].Origin != domain.OriginImported || rows[2].Origin != domain.OriginImported {
		t.Fatalf("imported origins = %q/%q, want %q", rows[0].Origin, rows[2].Origin, domain.OriginImported)
	}
	// The local row round-trips with the local origin and its plan intact.
	if rows[1].Origin != domain.OriginLocal {
		t.Fatalf("local origin = %q, want %q", rows[1].Origin, domain.OriginLocal)
	}
	if rows[1].PlanID != local.PlanID || rows[1].IdempotencyKey != local.IdempotencyKey {
		t.Fatalf("local row plan/key = %q/%q, want %q/%q", rows[1].PlanID, rows[1].IdempotencyKey, local.PlanID, local.IdempotencyKey)
	}
	if rows[0].PlanID != "" || rows[2].PlanID != "" {
		t.Fatalf("imported rows carry plan ids %q/%q, want empty", rows[0].PlanID, rows[2].PlanID)
	}

	// Offset and limit page through the created_at ordering.
	page, err := store.Export(ctx, workspaceID, 1, 1)
	if err != nil {
		t.Fatalf("Export(page) error = %v", err)
	}
	if len(page) != 1 || page[0].ID != local.ID {
		t.Fatalf("Export(offset 1, limit 1) = %v, want [%s]", page, local.ID)
	}

	// Another workspace exports nothing of this workspace's rows.
	empty, err := store.Export(ctx, randomID(t), 0, 50)
	if err != nil {
		t.Fatalf("Export(other workspace) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Export(other workspace) = %v, want none", empty)
	}
}

func TestPlanStoreJoinsAmbientTransaction(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewPlanStore(pool)
	runner := database.NewTxRunner(pool)
	workspaceID, todoID := randomID(t), randomID(t)

	// A failing unit of work rolls the plan back.
	failErr := errors.New("boom")
	err := runner.Run(ctx, func(txCtx context.Context) error {
		if err := store.Save(txCtx, newPlan(t, workspaceID, todoID, 1)); err != nil {
			return err
		}
		return failErr
	})
	if !errors.Is(err, failErr) {
		t.Fatalf("Run() error = %v, want failErr", err)
	}
	if statuses := statusesByTodo(t, pool, workspaceID, todoID); len(statuses) != 0 {
		t.Fatalf("plans after rollback = %#v, want none", statuses)
	}

	// A successful unit of work commits the plan.
	if err := runner.Run(ctx, func(txCtx context.Context) error {
		return store.Save(txCtx, newPlan(t, workspaceID, todoID, 1))
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if statuses := statusesByTodo(t, pool, workspaceID, todoID); statuses[1] != "planned" {
		t.Fatalf("plans after commit = %#v, want v1 planned", statuses)
	}
}
