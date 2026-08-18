package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTxRunnerCommitRollbackAndNesting(t *testing.T) {
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "..", "deploy", "migrations")
	if err := RunMigrations(ctx, url, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	pool, err := OpenPool(ctx, url)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	runner := NewTxRunner(pool)
	now := time.Now().UTC()

	commitID := randomInstanceID(t)
	if err := runner.Run(ctx, func(txCtx context.Context) error {
		return insertHeartbeat(txCtx, ExecutorFromContextOr(txCtx, pool), commitID, now)
	}); err != nil {
		t.Fatalf("Run() commit path error = %v", err)
	}
	assertHeartbeatExists(t, ctx, pool, commitID, true)
	t.Cleanup(func() { deleteHeartbeat(context.Background(), pool, commitID) })

	rollbackID := randomInstanceID(t)
	forcedErr := errors.New("forced failure")
	err = runner.Run(ctx, func(txCtx context.Context) error {
		if err := insertHeartbeat(txCtx, ExecutorFromContextOr(txCtx, pool), rollbackID, now); err != nil {
			return err
		}
		return forcedErr
	})
	if !errors.Is(err, forcedErr) {
		t.Fatalf("Run() rollback path error = %v, want %v", err, forcedErr)
	}
	assertHeartbeatExists(t, ctx, pool, rollbackID, false)

	err = runner.Run(ctx, func(txCtx context.Context) error {
		return runner.Run(txCtx, func(context.Context) error { return nil })
	})
	if !errors.Is(err, ErrAlreadyInTx) {
		t.Fatalf("nested Run() error = %v, want %v", err, ErrAlreadyInTx)
	}
}

func insertHeartbeat(ctx context.Context, executor Executor, instanceID string, now time.Time) error {
	_, err := executor.Exec(ctx, `
		insert into runtime.worker_heartbeats (instance_id, service_version, started_at, last_heartbeat_at)
		values ($1, $2, $3, $4)
	`, instanceID, "tx-test", now, now)
	return err
}

func assertHeartbeatExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instanceID string, want bool) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx,
		`select count(*) from runtime.worker_heartbeats where instance_id = $1`, instanceID,
	).Scan(&count); err != nil {
		t.Fatalf("heartbeat count query error = %v", err)
	}
	if (count == 1) != want {
		t.Fatalf("heartbeat %q exists = %v, want %v", instanceID, count == 1, want)
	}
}

func deleteHeartbeat(ctx context.Context, pool *pgxpool.Pool, instanceID string) {
	_, _ = pool.Exec(ctx, `delete from runtime.worker_heartbeats where instance_id = $1`, instanceID)
}

func randomInstanceID(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	return "tx-test-" + hex.EncodeToString(buf)
}
