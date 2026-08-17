package workerstatus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRegistryRecordAndLatest(t *testing.T) {
	if _, err := os.Stat(filepath.Join(registryMigrationsDirectory(), "001_create_runtime_health.sql")); err != nil {
		t.Fatalf("worker heartbeat migration is unavailable: %v", err)
	}
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	if err := database.RunMigrations(ctx, url, registryMigrationsDirectory()); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, "delete from runtime.worker_heartbeats where instance_id in ($1, $2)", "workerstatus-test-1", "workerstatus-test-2"); err != nil {
		t.Fatalf("clear test leases: %v", err)
	}

	now := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)
	registry := NewRegistry(pool, func() time.Time { return now })
	firstStartedAt := now.Add(-time.Minute)
	if err := registry.Record(ctx, Instance{ID: "workerstatus-test-1", Version: "abc", StartedAt: firstStartedAt}); err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}
	now = now.Add(time.Second)
	if err := registry.Record(ctx, Instance{ID: "workerstatus-test-2", Version: "def", StartedAt: now.Add(-2 * time.Minute)}); err != nil {
		t.Fatalf("Record(second) error = %v", err)
	}

	got, err := registry.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if got.InstanceID != "workerstatus-test-2" || got.Version != "def" || !got.LastHeartbeatAt.Equal(now) {
		t.Fatalf("Latest() = %#v, want latest second lease", got)
	}

	now = now.Add(time.Second)
	if err := registry.Record(ctx, Instance{ID: "workerstatus-test-1", Version: "updated", StartedAt: now.Add(-24 * time.Hour)}); err != nil {
		t.Fatalf("Record(upsert) error = %v", err)
	}
	var startedAt time.Time
	var version string
	if err := pool.QueryRow(ctx, "select started_at, service_version from runtime.worker_heartbeats where instance_id = $1", "workerstatus-test-1").Scan(&startedAt, &version); err != nil {
		t.Fatalf("query upserted lease: %v", err)
	}
	if !startedAt.Equal(firstStartedAt) || version != "updated" {
		t.Fatalf("upserted lease = started_at %s, version %q; want started_at %s, version %q", startedAt, version, firstStartedAt, "updated")
	}

	if err := registry.Remove(ctx, "workerstatus-test-1"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := registry.Remove(ctx, "workerstatus-test-2"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := registry.Latest(ctx); !errors.Is(err, ErrNoLease) {
		t.Fatalf("Latest() error = %v, want ErrNoLease", err)
	}
}

func registryMigrationsDirectory() string {
	return filepath.Join("..", "..", "..", "..", "deploy", "migrations")
}
