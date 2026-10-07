package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
	"time"
)

func TestSplitMustReconcileBeforeTargetFill(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	h.data.Snapshot.Actions = append(h.data.Snapshot.Actions, domain.CorporateAction{ID: "pre-open", InstrumentID: "fixture-01", Kind: "split", Currency: "USD", RatioNumerator: 2, RatioDenominator: 1, EffectiveAt: o.TargetOpenAt, AvailableAt: o.CreatedAt})
	h.at = o.TargetOpenAt.Add(9 * time.Hour)
	result, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.orders.GetFill(ctx, h.scope, h.account.ID, o.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("filled old quantities", result, e)
	}
	reconcile := h.reconciler()
	if _, e = reconcile.Handle(ctx, dto.ReconcileRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	state, e := h.orders.GetOrder(ctx, h.scope, h.account.ID, o.ID)
	if e != nil || state.State != domain.OrderCancelled {
		t.Fatal(state, e)
	}
}
