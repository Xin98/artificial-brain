package fakeoutbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

func TestOutboxWritesMessage(t *testing.T) {
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

	message := ports.OutboxMessage{Address: "+8613800137000", Channel: "sms", Purpose: "login", Code: "123456"}
	if err := New(pool).Write(ctx, message); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`select count(*) from identity.message_outbox where address = $1 and code = $2`,
		"+8613800137000", "123456",
	).Scan(&count); err != nil {
		t.Fatalf("count query error = %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox count = %d, want 1", count)
	}
}
