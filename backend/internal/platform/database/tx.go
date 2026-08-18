package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor runs queries against a pool, a single connection, or a transaction.
// Outbound adapters accept an Executor so they can participate in an ambient
// transaction without owning the connection lifecycle.
type Executor interface {
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Tx is a transactional Executor.
type Tx interface {
	Executor
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Begin starts a transaction on the pool.
func Begin(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	return pool.Begin(ctx)
}

// ErrAlreadyInTx is returned when a transaction is started inside another.
var ErrAlreadyInTx = errors.New("database: already in a transaction")

type executorContextKey struct{}

// WithExecutor returns a context carrying executor as the ambient executor.
func WithExecutor(ctx context.Context, executor Executor) context.Context {
	return context.WithValue(ctx, executorContextKey{}, executor)
}

// ExecutorFromContext returns the ambient executor and whether one was present.
func ExecutorFromContext(ctx context.Context) (Executor, bool) {
	executor, ok := ctx.Value(executorContextKey{}).(Executor)
	return executor, ok
}

// ExecutorFromContextOr returns the ambient executor when present, otherwise
// fallback (typically the caller's pool).
func ExecutorFromContextOr(ctx context.Context, fallback Executor) Executor {
	if executor, ok := ExecutorFromContext(ctx); ok {
		return executor
	}
	return fallback
}

// TxRunner executes a unit of work inside a single database transaction.
type TxRunner struct {
	pool *pgxpool.Pool
}

// NewTxRunner returns a TxRunner bound to pool.
func NewTxRunner(pool *pgxpool.Pool) *TxRunner {
	return &TxRunner{pool: pool}
}

// Run begins a transaction, invokes work with a context carrying that
// transaction as the ambient executor, and commits when work returns nil. Any
// error rolls the transaction back. Nesting is rejected with ErrAlreadyInTx so
// a composed unit of work always maps to exactly one transaction.
func (r *TxRunner) Run(ctx context.Context, work func(context.Context) error) error {
	if _, ok := ExecutorFromContext(ctx); ok {
		return ErrAlreadyInTx
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}

	if err := work(WithExecutor(ctx, tx)); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%w; rollback also failed: %v", err, rollbackErr)
		}
		return err
	}

	return tx.Commit(ctx)
}
