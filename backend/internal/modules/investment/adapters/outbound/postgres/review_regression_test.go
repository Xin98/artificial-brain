package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"reflect"
	"testing"
	"time"
)

func TestAutomaticEvaluationWaitsForDelayedInputs(t *testing.T) {
	for _, source := range []string{"bars", "financials"} {
		t.Run(source, func(t *testing.T) {
			h := newOrderHarness(t)
			ctx := context.Background()
			originalFacts := h.data.Snapshot.Facts
			date := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
			ready := time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)
			a, _ := h.accounts.Get(ctx, h.scope, h.account.ID)
			a.AutomationEnabled = true
			if e := h.accounts.Save(ctx, a, a.Version); e != nil {
				t.Fatal(e)
			}
			if source == "bars" {
				for n := range h.data.Snapshot.Bars {
					b := &h.data.Snapshot.Bars[n]
					if b.SessionDate.Equal(date) {
						b.AvailableAt = ready
					}
				}
			} else {
				h.data.Snapshot.Facts = nil
			}
			r := dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic", SessionDate: date}
			if _, e := h.evaluator().Prepare(ctx, r); e == nil {
				t.Fatal("incomplete input consumed immutable daily evaluation")
			}
			var count int
			if e := h.pool.QueryRow(ctx, "select count(*) from investment.evaluation_runs where account_id=$1", a.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal(count, e)
			}
			h.at = ready.Add(15 * time.Minute)
			h.data.Snapshot.Facts = originalFacts
			got, e := h.evaluator().Handle(ctx, r)
			if e != nil || !got.IssuedOrders {
				t.Fatal(got.Reason, e)
			}
			again, e := h.evaluator().Handle(ctx, r)
			if e != nil || again.ID != got.ID {
				t.Fatal(again, e)
			}
		})
	}
}

func TestRankingFailurePreservesAccountProtectionsAndAdvice(t *testing.T) {
	for _, scenario := range []string{"stop_loss", "drawdown_pause"} {
		t.Run(scenario, func(t *testing.T) {
			h := newOrderHarness(t)
			ctx := context.Background()
			a, _ := h.accounts.Get(ctx, h.scope, h.account.ID)
			cost := domain.Money(40000)
			if scenario == "drawdown_pause" {
				cost = 2000000
			}
			a.Balances.Available -= cost
			a.AutomationEnabled = true
			if e := h.accounts.Save(ctx, a, a.Version); e != nil {
				t.Fatal(e)
			}
			if e := h.orders.SavePositions(ctx, h.scope, a.ID, []domain.Position{{InstrumentID: "fixture-01", Industry: "manufacturing", Quantity: 10, CostBasis: cost}}); e != nil {
				t.Fatal(e)
			}
			h.data.Snapshot.Facts = nil
			if scenario == "stop_loss" {
				q := query.AnalysisQuery{Data: h.data, Accounts: h.accounts, Catalog: NewCatalogStore(h.pool), RiskBook: h.orders, Mode: "fixture", Now: func() time.Time { return h.at }}
				advice, e := q.Handle(ctx, dto.AnalysisRequest{Scope: h.scope, AccountID: a.ID, InstrumentID: "fixture-01"})
				if e != nil || advice.Recommendation.Action != "reduce_holding" {
					t.Errorf("missing holding reduction: %s %v", advice.Recommendation.Action, e)
				}
			}
			got, e := h.evaluator().Handle(ctx, dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic"})
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "stop_loss" {
				if !got.IssuedOrders {
					t.Fatal("ranking loss suppressed stop loss", got.Reason)
				}
				orders, e := h.orders.ListPending(ctx, h.scope, a.ID)
				if e != nil || len(orders) != 1 || orders[0].Side != "sell" || orders[0].Reason != "stop_loss" {
					t.Fatal(orders, e)
				}
			} else {
				a, e = h.accounts.Get(ctx, h.scope, a.ID)
				if e != nil || a.AutomationEnabled || a.PauseReason != "drawdown_pause" {
					t.Fatal(a.PauseReason, e)
				}
			}
		})
	}
}

type captureRefresh struct {
	ids     []string
	failure error
}

func (s *captureRefresh) Sync(_ context.Context, r dto.SyncRequest) error {
	s.ids = append(s.ids, r.InstrumentIDs...)
	return s.failure
}

type realRefreshData struct{ data *fixture.Adapter }

func (r realRefreshData) Read(ctx context.Context, _ string, at time.Time) (domain.Snapshot, error) {
	s, e := r.data.Read(ctx, "fixture", at)
	s.Mode = "alpaca_sec"
	s.Feed = "sip"
	s.DatasetVersion = "alpaca_sec/sip/v1"
	return s, e
}
func TestRealRefreshIncludesHoldingsOrdersAndPendingUniverse(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	data := realRefreshData{h.data}
	catalog := NewCatalogStore(h.pool)
	create := command.CreateAccountHandler{Mutations: h.place.Mutations, Accounts: h.accounts, Catalog: catalog, Ledger: NewLedgerStore(h.pool), Data: data, Now: func() time.Time { return h.at }, NewID: uuid}
	var e error
	h.account, e = create.Handle(ctx, dto.CreateAccountRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "accounts", Key: uuid()}, Mode: "alpaca_sec"})
	if e != nil {
		t.Fatal(e)
	}
	h.place.Data = data
	h.buy(t, "1")
	a, _ := h.accounts.Get(ctx, h.scope, h.account.ID)
	for _, id := range []string{"fixture-03", "fixture-04"} {
		v := domain.UniverseVersion{ID: uuid(), UniverseID: uuid(), Scope: h.scope, Name: id, Mode: "fixture", InstrumentIDs: []string{id}, CreatedAt: h.at, EffectiveAt: h.at}
		if e := catalog.InsertUniverse(ctx, h.scope, v); e != nil {
			t.Fatal(e)
		}
		if id == "fixture-03" {
			a.UniverseVersionID = v.ID
		} else {
			a.PendingConfig = &domain.AccountConfig{UniverseVersionID: v.ID, StrategyVersionID: a.StrategyVersionID, Policy: a.Policy, EffectiveAt: h.at.Add(24 * time.Hour)}
		}
	}
	if e := h.accounts.Save(ctx, a, a.Version); e != nil {
		t.Fatal(e)
	}
	if e := h.orders.SavePositions(ctx, h.scope, a.ID, []domain.Position{{InstrumentID: "fixture-02", Industry: "manufacturing", Quantity: 5, CostBasis: 10000}}); e != nil {
		t.Fatal(e)
	}
	sentinel := errors.New("captured refresh")
	source := &captureRefresh{failure: sentinel}
	jobs := command.InvestmentJobHandler{UOW: database.NewTxRunner(h.pool), Accounts: h.accounts, Orders: h.orders, SyncRuns: NewRunStore(h.pool), Source: source, Reconcile: command.ReconcileAccountHandler{Catalog: catalog}, Now: func() time.Time { return h.at }, NewID: uuid}
	e = jobs.HandleJob(ctx, dto.InvestmentJobArgs{JobType: "account", WorkspaceID: h.scope.WorkspaceID, OwnerUserID: h.scope.OwnerUserID, AccountID: a.ID}, false)
	if !errors.Is(e, sentinel) || !reflect.DeepEqual(source.ids, []string{"fixture-01", "fixture-02", "fixture-03", "fixture-04"}) {
		t.Fatal(source.ids, e)
	}
}
