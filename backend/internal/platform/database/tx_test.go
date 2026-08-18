package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubExecutor struct {
	name string
}

func (s stubExecutor) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (s stubExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (s stubExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func TestExecutorFromContextReturnsAmbientExecutor(t *testing.T) {
	ambient := stubExecutor{name: "ambient"}
	fallback := stubExecutor{name: "fallback"}

	ctx := WithExecutor(context.Background(), ambient)
	got, ok := ExecutorFromContext(ctx)
	if !ok {
		t.Fatalf("ExecutorFromContext() ok = false, want true")
	}
	if stub, isStub := got.(stubExecutor); !isStub || stub.name != "ambient" {
		t.Fatalf("ExecutorFromContext() = %v, want ambient executor", got)
	}

	if executor := ExecutorFromContextOr(ctx, fallback); executor != ambient {
		t.Fatalf("ExecutorFromContextOr() = %v, want %v", executor, ambient)
	}
}

func TestExecutorFromContextFallsBackWhenAbsent(t *testing.T) {
	fallback := stubExecutor{name: "fallback"}

	if _, ok := ExecutorFromContext(context.Background()); ok {
		t.Fatalf("ExecutorFromContext() ok = true on empty context, want false")
	}
	if executor := ExecutorFromContextOr(context.Background(), fallback); executor != fallback {
		t.Fatalf("ExecutorFromContextOr() = %v, want %v", executor, fallback)
	}
}

func TestTxRunnerRejectsNestedTransaction(t *testing.T) {
	runner := NewTxRunner(nil)
	ctx := WithExecutor(context.Background(), stubExecutor{name: "ambient"})

	err := runner.Run(ctx, func(context.Context) error { return nil })
	if !errors.Is(err, ErrAlreadyInTx) {
		t.Fatalf("Run() error = %v, want %v", err, ErrAlreadyInTx)
	}
}
