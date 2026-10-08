package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"reflect"
	"testing"
	"time"
)

func TestBacktestMatchesDailyPaperLedger(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	prior, _ := h.data.Snapshot.Calendar.Session(from.AddDate(0, 0, -1))
	h.at = prior.CloseAt.Add(30 * time.Minute)
	create := command.CreateAccountHandler{Mutations: h.place.Mutations, Accounts: h.accounts, Catalog: NewCatalogStore(h.pool), Ledger: NewLedgerStore(h.pool), Data: h.data, Now: func() time.Time { return h.at }, NewID: uuid}
	a, e := create.Handle(ctx, dto.CreateAccountRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "accounts", Key: uuid()}, Mode: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	h.account = a
	account, e := h.accounts.Get(ctx, h.scope, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	account.AutomationEnabled = true
	if e = h.accounts.Save(ctx, account, account.Version); e != nil {
		t.Fatal(e)
	}
	catalog := NewCatalogStore(h.pool)
	u, e := catalog.GetUniverse(ctx, h.scope, a.UniverseVersionID)
	if e != nil {
		t.Fatal(e)
	}
	strategy, e := catalog.GetStrategy(ctx, h.scope, a.StrategyVersionID)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := domain.Replay(domain.BacktestInput{Snapshots: []domain.Snapshot{h.data.Snapshot}, Calendar: h.data.Snapshot.Calendar, Universe: u, Strategy: strategy, InitialCash: account.InitialCash, From: from, To: to, BenchmarkID: "fixture-spy"})
	if e != nil {
		t.Fatal(e)
	}
	evaluator := h.evaluator()
	if _, e = evaluator.Handle(ctx, dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic", SessionDate: prior.Date}); e != nil {
		t.Fatal(e)
	}
	reconcile := command.ReconcileAccountHandler{UOW: database.NewTxRunner(h.pool), Accounts: h.accounts, Orders: h.orders, Ledger: NewLedgerStore(h.pool), Catalog: catalog, Data: h.data, Now: func() time.Time { return h.at }, NewID: uuid}
	curve := []domain.NAVPoint{}
	for _, point := range replay.Curve {
		session, _ := h.data.Snapshot.Calendar.Session(point.SessionDate)
		h.at = session.CloseAt.Add(30 * time.Minute)
		if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
			t.Fatal(e)
		}
		if _, e = reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
			t.Fatal(e)
		}
		current, e := h.accounts.Get(ctx, h.scope, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		positions, e := h.orders.LoadPositions(ctx, h.scope, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		data, e := h.data.Read(ctx, "fixture", h.at)
		if e != nil {
			t.Fatal(e)
		}
		nav, e := domain.ComputeNAV(current, positions, data, nil)
		if e != nil {
			t.Fatal(e)
		}
		curve = append(curve, domain.NAVPoint{SessionDate: session.Date, NAV: nav})
		if e = NewRunStore(h.pool).SaveNAV(ctx, h.scope, a.ID, curve[len(curve)-1], 0, h.at, nil); e != nil {
			t.Fatal(e)
		}
		if session.Date.Before(to) {
			if _, e = evaluator.Handle(ctx, dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic", SessionDate: session.Date}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if !reflect.DeepEqual(curve, replay.Curve) {
		for n, v := range curve {
			if v.NAV != replay.Curve[n].NAV {
				t.Fatalf("day %s paper=%s replay=%s", v.SessionDate, v.NAV.String(), replay.Curve[n].NAV.String())
			}
		}
	}
	var fillCount int
	if e = h.pool.QueryRow(ctx, "select count(*) from investment.fills where account_id=$1", a.ID).Scan(&fillCount); e != nil || fillCount != len(replay.Fills) {
		t.Fatal("different fill count", fillCount, len(replay.Fills), e)
	}
}
