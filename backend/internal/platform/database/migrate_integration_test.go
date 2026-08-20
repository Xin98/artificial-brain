package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMigrate(t *testing.T) {
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "..", "deploy", "migrations")
	for range 2 {
		if err := RunMigrations(ctx, url, directory); err != nil {
			t.Fatalf("RunMigrations() error = %v", err)
		}
	}

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("pgx.Connect() error = %v", err)
	}
	t.Cleanup(func() { conn.Close(ctx) })

	var version int32
	if err := conn.QueryRow(ctx, "select version from public.schema_version limit 1").Scan(&version); err != nil {
		t.Fatalf("schema version query error = %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, CurrentSchemaVersion)
	}
	if version != 7 {
		t.Fatalf("schema version = %d, want 7", version)
	}

	var workerTableCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from information_schema.tables
		where table_schema = 'runtime' and table_name = 'worker_heartbeats'
	`).Scan(&workerTableCount); err != nil {
		t.Fatalf("worker table query error = %v", err)
	}
	if workerTableCount != 1 {
		t.Fatalf("runtime.worker_heartbeats count = %d, want 1", workerTableCount)
	}

	expectedTables := [][2]string{
		{"identity", "workspaces"},
		{"identity", "users"},
		{"identity", "login_challenges"},
		{"identity", "sessions"},
		{"identity", "contact_channels"},
		{"identity", "message_outbox"},
		{"todo", "todos"},
		{"reminder", "reminder_plans"},
		{"reminder", "reminder_deliveries"},
		{"reminder", "fake_outbox"},
		{"conversation", "confirmation_requests"},
		{"conversation", "messages"},
		{"public", "river_job"},
	}
	for _, schemaTable := range expectedTables {
		var count int
		if err := conn.QueryRow(ctx, `
			select count(*)
			from information_schema.tables
			where table_schema = $1 and table_name = $2
		`, schemaTable[0], schemaTable[1]).Scan(&count); err != nil {
			t.Fatalf("table query error for %s.%s = %v", schemaTable[0], schemaTable[1], err)
		}
		if count != 1 {
			t.Fatalf("%s.%s count = %d, want 1", schemaTable[0], schemaTable[1], count)
		}
	}
}
