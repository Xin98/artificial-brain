package postgres

import (
	"context"
	"errors"
	scheduler "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/river"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"reflect"
	"testing"
	"time"
)

func TestBacktestIsolatesPaperAccount(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	tx := database.NewTxRunner(h.pool)
	client, e := riverqueue.NewClient(riverpgxv5.New(h.pool), &riverqueue.Config{})
	if e != nil {
		t.Fatal(e)
	}
	var _ *riverqueue.Client[pgx.Tx] = client
	runs := NewRunStore(h.pool)
	snapshots := NewSnapshotStore(h.pool)
	catalog := NewCatalogStore(h.pool)
	create := command.CreateBacktestHandler{Mutations: application.MutationExecutor{UOW: tx, Requests: NewRequestStore(h.pool)}, Runs: runs, Snapshots: snapshots, Catalog: catalog, History: h.data, Scheduler: &scheduler.Scheduler{Client: client}, Mode: "fixture", Now: func() time.Time { return h.at }, NewID: uuid}
	r := dto.CreateBacktestRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "backtests", Key: uuid()}, StrategyVersionID: h.account.StrategyVersionID, UniverseVersionID: h.account.UniverseVersionID, From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)}
	before, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	v, e := create.Handle(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	same, e := create.Handle(ctx, r)
	if e != nil || v.ID != same.ID {
		t.Fatal(e, same)
	}
	t.Cleanup(func() {
		h.pool.Exec(context.Background(), "delete from river_job where kind='investment_job' and args->>'RunID'=$1", v.ID)
	})
	other := h.scope
	other.OwnerUserID = uuid()
	if _, e = runs.GetBacktest(ctx, other, v.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	frozen, e := runs.GetBacktest(ctx, h.scope, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	source, e := snapshots.Get(ctx, frozen.DatasetVersion, frozen.SnapshotIDs[0])
	if e != nil {
		t.Fatal(e)
	}
	// Freeze must retain original and amended accessions for historical cutoffs.
	prior, revised := false, false
	for _, f := range source.Facts {
		if f.InstrumentID == "fixture-01" && f.Concept == "NetIncome" {
			if f.Accession == "fixture-amendment-01" {
				revised = true
			} else {
				prior = true
			}
		}
	}
	if !prior || !revised {
		t.Fatal("historical financial sources missing")
	}
	run := command.RunBacktestHandler{UOW: tx, Runs: runs, Catalog: catalog, Snapshots: snapshots, Now: func() time.Time { return h.at }}
	view, e := run.Handle(ctx, dto.RunBacktestRequest{Scope: h.scope, RunID: v.ID})
	if e != nil || view.Status != "completed" {
		t.Fatal(view, e)
	}
	again, e := run.Handle(ctx, dto.RunBacktestRequest{Scope: h.scope, RunID: v.ID})
	if e != nil || again != view {
		t.Fatal(again, e)
	}
	after, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("paper account changed", e)
	}
	var orders, ledger int
	h.pool.QueryRow(ctx, "select count(*) from investment.orders where account_id=$1", h.account.ID).Scan(&orders)
	h.pool.QueryRow(ctx, "select count(*) from investment.ledger_entries where account_id=$1", h.account.ID).Scan(&ledger)
	if orders != 0 || ledger != 1 {
		t.Fatal(orders, ledger)
	}
	result, e := runs.GetBacktest(ctx, h.scope, v.ID)
	if e != nil || result.Result == nil || len(result.Result.Curve) < 20 {
		t.Fatal(e)
	}
}
