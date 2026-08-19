package fake

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	reminderpostgres "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/postgres"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

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
	if _, err := pool.Exec(ctx, `truncate reminder.fake_outbox`); err != nil {
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

func TestOutboxWriteAndLatestByAddress(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	outbox := NewOutbox(pool)
	reader := reminderpostgres.NewOutboxReader(pool)
	addressA := "alice+" + randomID(t) + "@example.com"
	addressB := "bob+" + randomID(t) + "@example.com"
	todoA := randomID(t)

	// Seven writes to address A, one to address B.
	for i := 1; i <= 7; i++ {
		if err := outbox.Write(ctx, "email", addressA, todoA, fmt.Sprintf("reminder body %d", i)); err != nil {
			t.Fatalf("Write(%d) error = %v", i, err)
		}
	}
	if err := outbox.Write(ctx, "sms", addressB, randomID(t), "bob body"); err != nil {
		t.Fatalf("Write(bob) error = %v", err)
	}

	// Latest five for address A, newest first.
	rows, err := reader.LatestByAddress(ctx, addressA, 5)
	if err != nil {
		t.Fatalf("LatestByAddress() error = %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("LatestByAddress() returned %d rows, want 5", len(rows))
	}
	wantBodies := []string{
		"reminder body 7", "reminder body 6", "reminder body 5", "reminder body 4", "reminder body 3",
	}
	for i, row := range rows {
		if row.Body != wantBodies[i] {
			t.Fatalf("row %d body = %q, want %q", i, row.Body, wantBodies[i])
		}
		if row.Address != addressA || row.Channel != "email" || row.TodoID != todoA {
			t.Fatalf("row %d = %#v, want address %s channel email todo %s", i, row, addressA, todoA)
		}
		if row.CreatedAt.IsZero() {
			t.Fatalf("row %d created_at is zero", i)
		}
		if i > 0 && rows[i-1].CreatedAt.Before(row.CreatedAt) {
			t.Fatalf("rows not ordered created_at desc: %v then %v", rows[i-1].CreatedAt, row.CreatedAt)
		}
	}

	// Address isolation: address B sees only its own message.
	otherRows, err := reader.LatestByAddress(ctx, addressB, 5)
	if err != nil {
		t.Fatalf("LatestByAddress(bob) error = %v", err)
	}
	if len(otherRows) != 1 || otherRows[0].Body != "bob body" || otherRows[0].Channel != "sms" {
		t.Fatalf("LatestByAddress(bob) = %#v, want the single bob row", otherRows)
	}

	// An address with no messages lists nothing.
	if empty, err := reader.LatestByAddress(ctx, "nobody+"+randomID(t)+"@example.com", 5); err != nil {
		t.Fatalf("LatestByAddress(nobody) error = %v", err)
	} else if len(empty) != 0 {
		t.Fatalf("LatestByAddress(nobody) = %#v, want none", empty)
	}
}

func TestOutboxWriteJoinsAmbientTransaction(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	outbox := NewOutbox(pool)
	reader := reminderpostgres.NewOutboxReader(pool)
	runner := database.NewTxRunner(pool)
	address := "carol+" + randomID(t) + "@example.com"
	todoID := randomID(t)

	failErr := errors.New("boom")
	err := runner.Run(ctx, func(txCtx context.Context) error {
		if err := outbox.Write(txCtx, "email", address, todoID, "rolled back body"); err != nil {
			return err
		}
		return failErr
	})
	if !errors.Is(err, failErr) {
		t.Fatalf("Run() error = %v, want failErr", err)
	}
	if rows, err := reader.LatestByAddress(ctx, address, 5); err != nil {
		t.Fatalf("LatestByAddress() error = %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("rows after rollback = %#v, want none", rows)
	}

	if err := runner.Run(ctx, func(txCtx context.Context) error {
		return outbox.Write(txCtx, "email", address, todoID, "committed body")
	}); err != nil {
		t.Fatalf("Run(commit) error = %v", err)
	}
	rows, err := reader.LatestByAddress(ctx, address, 5)
	if err != nil {
		t.Fatalf("LatestByAddress() after commit error = %v", err)
	}
	if len(rows) != 1 || rows[0].Body != "committed body" {
		t.Fatalf("rows after commit = %#v, want the committed row", rows)
	}
}
