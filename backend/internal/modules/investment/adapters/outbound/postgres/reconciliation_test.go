package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"sync"
	"testing"
	"time"
)

func (h *orderHarness) reconciler() command.ReconcileAccountHandler {
	return command.ReconcileAccountHandler{UOW: database.NewTxRunner(h.pool), Accounts: h.accounts, Orders: h.orders, Ledger: NewLedgerStore(h.pool), Catalog: NewCatalogStore(h.pool), Data: h.data, Now: func() time.Time { return h.at }, NewID: uuid}
}
func TestSplitPreservesCostAndCancelsPending(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	bought := h.buy(t, "10")
	h.at = bought.TargetOpenAt.Add(9 * time.Hour)
	if _, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	old := h.buy(t, "5")
	h.at = old.TargetOpenAt.Add(time.Minute)
	action := domain.CorporateAction{ID: "split-test", InstrumentID: "fixture-01", Kind: "split", Currency: "USD", RatioNumerator: 2, RatioDenominator: 1, EffectiveAt: old.TargetOpenAt, AvailableAt: old.CreatedAt}
	dividend := domain.CorporateAction{ID: "dividend-test", InstrumentID: "fixture-01", Kind: "dividend", Currency: "USD", Amount: 1000000, EffectiveAt: old.TargetOpenAt, AvailableAt: old.CreatedAt, PayAt: old.TargetOpenAt.AddDate(0, 0, 4)}
	h.data.Snapshot.Actions = append(h.data.Snapshot.Actions, action, dividend)
	reconcile := h.reconciler()
	result, e := reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID})
	if e != nil || result.State != "completed" {
		t.Fatal(result, e)
	}
	positions, e := h.orders.LoadPositions(ctx, h.scope, h.account.ID)
	if e != nil || positions[0].Quantity != 18 || positions[0].CostBasis != 25283 {
		t.Fatal(positions, e)
	}
	order, e := h.orders.GetOrder(ctx, h.scope, h.account.ID, old.ID)
	if e != nil || order.State != domain.OrderCancelled {
		t.Fatal(order, e)
	}
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil || a.Balances.Reserved != 0 || a.Balances.Dividends != 1800 {
		t.Fatal(a, e)
	}
	before, _ := a.Balances.Total()
	h.at = dividend.PayAt
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	a, e = h.accounts.Get(ctx, h.scope, h.account.ID)
	after, _ := a.Balances.Total()
	if e != nil || a.Balances.Dividends != 0 || before != after {
		t.Fatal(a, before, after, e)
	}
	var count int
	e = h.pool.QueryRow(ctx, "select count(*) from investment.ledger_entries where account_id=$1 and event_key=$2", a.ID, "payment/"+dividend.ID).Scan(&count)
	if e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
func TestSettlementDatabaseUsesOriginalTradeAndSeparateCalendar(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	bought := h.buy(t, "10")
	h.at = bought.TargetOpenAt.Add(9 * time.Hour)
	if _, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	h.at = time.Date(2026, 10, 8, 21, 0, 0, 0, time.UTC)
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	sold, e := h.place.Handle(ctx, dto.PlaceOrderRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "orders/" + a.ID, Key: uuid(), ExpectedVersion: a.Version}, AccountID: a.ID, InstrumentID: "fixture-01", Side: "sell", Quantity: "3"})
	if e != nil {
		t.Fatal(e)
	}
	h.at = sold.TargetOpenAt.Add(9 * time.Hour)
	if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
		t.Fatal(e)
	}
	fill, e := h.orders.GetFill(ctx, h.scope, a.ID, sold.ID)
	if e != nil {
		t.Fatal(e)
	}
	if fill.SettlesAt.UTC().Day() != 13 || fill.SettlesAt.UTC().Hour() != 4 {
		t.Fatal(fill.SettlesAt)
	}
	h.at = time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC)
	reconcile := h.reconciler()
	if _, e = reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
		t.Fatal(e)
	}
	a, e = h.accounts.Get(ctx, h.scope, a.ID)
	if e != nil || a.Balances.Unsettled != fill.Gross-fill.Fee {
		t.Fatal(a, e)
	}
	h.at = fill.SettlesAt
	if _, e = reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
		t.Fatal(e)
	}
	paid, e := h.accounts.Get(ctx, h.scope, a.ID)
	if e != nil || paid.Balances.Unsettled != 0 || paid.Balances.Available != a.Balances.Available+fill.Gross-fill.Fee {
		t.Fatal(paid, e)
	}
	if _, e = reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
		t.Fatal(e)
	}
	var count int
	h.pool.QueryRow(ctx, "select count(*) from investment.ledger_entries where account_id=$1 and kind='settlement'", a.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
