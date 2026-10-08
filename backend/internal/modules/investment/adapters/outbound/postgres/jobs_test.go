package postgres

import (
	"context"
	"errors"
	scheduler "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/river"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"testing"
	"time"
)

func TestInvestmentInsertTxRollback(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	client, e := riverqueue.NewClient(riverpgxv5.New(p), &riverqueue.Config{})
	if e != nil {
		t.Fatal(e)
	}
	s := &scheduler.Scheduler{Client: client}
	args := dto.InvestmentJobArgs{JobType: "sync", RunID: uuid(), WorkspaceID: uuid(), OwnerUserID: uuid()}
	if _, e = s.Enqueue(ctx, args); !errors.Is(e, scheduler.ErrNoAmbientTransaction) {
		t.Fatal(e)
	}
	abort := errors.New("abort")
	var jobID int64
	e = database.NewTxRunner(p).Run(ctx, func(ctx context.Context) error {
		var err error
		jobID, err = s.Enqueue(ctx, args)
		if err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(e, abort) {
		t.Fatal(e)
	}
	var found bool
	if e = p.QueryRow(ctx, "select exists(select 1 from river_job where id=$1)", jobID).Scan(&found); e != nil || found {
		t.Fatal(e, found)
	}
	e = database.NewTxRunner(p).Run(ctx, func(ctx context.Context) error { var err error; jobID, err = s.Enqueue(ctx, args); return err })
	if e != nil {
		t.Fatal(e)
	}
	if e = p.QueryRow(ctx, "select exists(select 1 from river_job where id=$1)", jobID).Scan(&found); e != nil || !found {
		t.Fatal(e, found)
	}
	t.Cleanup(func() { p.Exec(context.Background(), "delete from river_job where id=$1", jobID) })
	var _ *riverqueue.Client[pgx.Tx] = client
}
func TestTickEarlyCloseDSTAndRecovery(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	date := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	session, e := h.data.Snapshot.Calendar.Session(date)
	if e != nil {
		t.Fatal(e)
	}
	if session.CloseAt.Hour() != 18 {
		t.Fatal("early close missing", session)
	}
	dst, _ := h.data.Snapshot.Calendar.Session(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if dst.OpenAt.Hour() != 14 || dst.OpenAt.Minute() != 30 {
		t.Fatal(dst)
	}
	r := dto.EvaluateRequest{Scope: h.scope, AccountID: h.account.ID, Purpose: "automatic", SessionDate: date}
	h.at = session.CloseAt.Add(29 * time.Minute)
	if _, e = h.evaluator().Prepare(ctx, r); !errors.Is(e, domain.ErrDataStale) {
		t.Fatal(e)
	}
	h.at = session.CloseAt.Add(30 * time.Minute)
	first, e := h.evaluator().Prepare(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	h.at = h.at.Add(15 * time.Minute)
	again, e := h.evaluator().Prepare(ctx, r)
	if e != nil || first.ID != again.ID {
		t.Fatal(e, again)
	}
}
func TestWorkerPauseStillReconcilesEffectiveOrders(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	h.at = o.TargetOpenAt.Add(9 * time.Hour)
	evaluator := h.evaluator()
	ledger := NewLedgerStore(h.pool)
	jobs := command.InvestmentJobHandler{UOW: database.NewTxRunner(h.pool), Accounts: h.accounts, Orders: h.orders, Runs: NewRunStore(h.pool), Ledger: ledger, Data: h.data, Evaluate: &evaluator, Execute: h.execute, Reconcile: command.ReconcileAccountHandler{UOW: database.NewTxRunner(h.pool), Accounts: h.accounts, Orders: h.orders, Ledger: ledger, Catalog: NewCatalogStore(h.pool), Data: h.data, Now: func() time.Time { return h.at }, NewID: uuid}, Now: func() time.Time { return h.at }, NewID: uuid}
	args := dto.InvestmentJobArgs{JobType: "account", WorkspaceID: h.scope.WorkspaceID, OwnerUserID: h.scope.OwnerUserID, AccountID: h.account.ID}
	for n := 0; n < 2; n++ {
		if e := jobs.HandleJob(ctx, args, false); e != nil {
			t.Fatal(e)
		}
	}
	var fills, evals int
	if e := h.pool.QueryRow(ctx, "select count(*) from investment.fills where account_id=$1", h.account.ID).Scan(&fills); e != nil {
		t.Fatal(e)
	}
	if e := h.pool.QueryRow(ctx, "select count(*) from investment.evaluation_runs where account_id=$1", h.account.ID).Scan(&evals); e != nil {
		t.Fatal(e)
	}
	if fills != 1 || evals != 0 {
		t.Fatal(fills, evals)
	}
}
