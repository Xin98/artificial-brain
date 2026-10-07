package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
	"time"
)

func TestDividendWaitsForPriorLateOpeningFill(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	exDate := o.TargetOpenAt.AddDate(0, 0, 1)
	action := domain.CorporateAction{ID: "late-dividend", InstrumentID: "fixture-01", Kind: "dividend", Currency: "USD", Amount: 1000000, EffectiveAt: exDate, AvailableAt: o.CreatedAt, PayAt: exDate.AddDate(0, 0, 4)}
	h.data.Snapshot.Actions = append(h.data.Snapshot.Actions, action)
	for n := range h.data.Snapshot.Bars {
		b := &h.data.Snapshot.Bars[n]
		if b.InstrumentID == "fixture-01" && b.SessionDate.Format("2006-01-02") == o.TargetOpenAt.Format("2006-01-02") {
			b.AvailableAt = exDate.Add(time.Hour)
		}
	}
	h.at = exDate
	reconcile := h.reconciler()
	blocked, e := reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID})
	if e != nil || blocked.State != "blocked" {
		t.Fatal(blocked, e)
	}
	h.at = exDate.Add(2 * time.Hour)
	if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	result, e := reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID})
	if e != nil || result.State != "completed" {
		t.Fatal(result, e)
	}
	account, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil || account.Balances.Dividends != 900 {
		t.Fatal(account, e)
	}
	h.at = action.PayAt
	for n := 0; n < 2; n++ {
		if _, e = reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
			t.Fatal(e)
		}
	}
	account, e = h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil || account.Balances.Dividends != 0 {
		t.Fatal(account, e)
	}
	var count int
	h.pool.QueryRow(ctx, "select count(*) from investment.ledger_entries where account_id=$1 and event_key=$2", h.account.ID, "payment/"+action.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestLateSplitAfterCompletedFillBlocksInsteadOfGuessing(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	h.at = o.TargetOpenAt.Add(9 * time.Hour)
	if _, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	action := domain.CorporateAction{ID: "late-split", InstrumentID: "fixture-01", Kind: "split", Currency: "USD", RatioNumerator: 2, RatioDenominator: 1, EffectiveAt: o.TargetOpenAt, AvailableAt: h.at}
	h.data.Snapshot.Actions = append(h.data.Snapshot.Actions, action)
	reconcile := h.reconciler()
	result, e := reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID})
	if e != nil || result.State != "blocked" || len(result.Blocked) != 1 || result.Blocked[0].Reason != "late_split_inventory_replay_requires_review" {
		t.Fatal(result, e)
	}
	positions, e := h.orders.LoadPositions(ctx, h.scope, h.account.ID)
	if e != nil || positions[0].Quantity != 9 || positions[0].CostBasis != 25283 {
		t.Fatal(positions, e)
	}
}
func TestStopLossIntentSurvivesAwaitingBar(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	b := h.buy(t, "10")
	h.at = b.TargetOpenAt.Add(9 * time.Hour)
	if _, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	sell, e := h.place.Handle(ctx, dto.PlaceOrderRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "orders/" + a.ID, Key: uuid(), ExpectedVersion: a.Version}, AccountID: a.ID, InstrumentID: "fixture-01", Side: "sell", Quantity: "9"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.pool.Exec(ctx, "update investment.orders set reason='stop_loss' where id=$1", sell.ID); e != nil {
		t.Fatal(e)
	}
	a, e = h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.Policy.TurnoverLimit = .0001
	if e = h.accounts.Save(ctx, a, a.Version); e != nil {
		t.Fatal(e)
	}
	h.at = sell.TargetOpenAt.Add(time.Minute)
	if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
		t.Fatal(e)
	}
	waiting, e := h.orders.GetOrder(ctx, h.scope, a.ID, sell.ID)
	if e != nil || waiting.Reason != "stop_loss" || waiting.State != domain.OrderAwaitingBar {
		t.Fatal(waiting, e)
	}
	h.at = sell.TargetOpenAt.Add(9 * time.Hour)
	if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: a.ID}); e != nil {
		t.Fatal(e)
	}
	filled, e := h.orders.GetFill(ctx, h.scope, a.ID, sell.ID)
	if e != nil || filled.Quantity != 9 {
		t.Fatal(filled, e)
	}
}
