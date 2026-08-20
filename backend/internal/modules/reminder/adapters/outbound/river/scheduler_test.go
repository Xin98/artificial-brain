package river

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

var _ ports.JobScheduler = (*Scheduler)(nil)

// nonTxExecutor satisfies database.Executor but is not a pgx.Tx.
type nonTxExecutor struct{}

func (nonTxExecutor) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (nonTxExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (nonTxExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not implemented")
}

func TestScheduleWithoutAmbientTransactionFails(t *testing.T) {
	scheduler := New(nil)

	_, err := scheduler.Schedule(context.Background(), ports.ReminderJob{Channels: []string{"email"}})
	if !errors.Is(err, ErrNoAmbientTransaction) {
		t.Fatalf("Schedule() error = %v, want ErrNoAmbientTransaction", err)
	}
}

func TestScheduleWithNonTransactionExecutorFails(t *testing.T) {
	scheduler := New(nil)
	ctx := database.WithExecutor(context.Background(), nonTxExecutor{})

	_, err := scheduler.Schedule(ctx, ports.ReminderJob{Channels: []string{"email"}})
	if !errors.Is(err, ErrExecutorNotTransaction) {
		t.Fatalf("Schedule() error = %v, want ErrExecutorNotTransaction", err)
	}
}
