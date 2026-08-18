package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
	if _, err := pool.Exec(ctx, `truncate reminder.reminder_plans`); err != nil {
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
