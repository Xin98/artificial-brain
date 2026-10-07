package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
	"time"
)

func TestFillIgnoresTargetCloseVolumeAndFutureFacts(t *testing.T) {
	baseline := newOrderHarness(t)
	altered := newOrderHarness(t)
	first := baseline.buy(t, "10")
	second := altered.buy(t, "10")
	baseline.at = first.TargetOpenAt.Add(9 * time.Hour)
	altered.at = second.TargetOpenAt.Add(9 * time.Hour)
	for n := range altered.data.Snapshot.Bars {
		b := &altered.data.Snapshot.Bars[n]
		if b.SessionDate.Format("2006-01-02") == second.TargetOpenAt.Format("2006-01-02") {
			b.High = 999999999
			b.Low = 1
			b.Close = 500000000
			b.Volume = 9000000000
		}
	}
	for n := range altered.data.Snapshot.Facts {
		f := &altered.data.Snapshot.Facts[n]
		if f.AvailableAt.After(second.TargetOpenAt) {
			f.Value = "999999999999"
		}
	}
	for _, h := range []*orderHarness{baseline, altered} {
		if _, e := h.execute.Handle(context.Background(), dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
			t.Fatal(e)
		}
	}
	a, e := baseline.orders.GetFill(context.Background(), baseline.scope, baseline.account.ID, first.ID)
	if e != nil {
		t.Fatal(e)
	}
	b, e := altered.orders.GetFill(context.Background(), altered.scope, altered.account.ID, second.ID)
	if e != nil || a.Quantity != b.Quantity || a.Price != b.Price || a.Gross != b.Gross || a.Fee != b.Fee {
		t.Fatal(a, b, e)
	}
}
func TestLateTargetBarCannotReviveExpiredOrder(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	for n := range h.data.Snapshot.Bars {
		b := &h.data.Snapshot.Bars[n]
		if b.InstrumentID == "fixture-01" && b.SessionDate.Format("2006-01-02") == o.TargetOpenAt.Format("2006-01-02") {
			b.AvailableAt = o.ExpiresAt.Add(time.Second)
		}
	}
	h.at = o.ExpiresAt.Add(2 * time.Second)
	if _, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	state, e := h.orders.GetOrder(ctx, h.scope, h.account.ID, o.ID)
	if e != nil || state.State != domain.OrderExpired {
		t.Fatal(state, e)
	}
	// A later source correction cannot reverse an already terminal expiry.
	for n := range h.data.Snapshot.Bars {
		b := &h.data.Snapshot.Bars[n]
		if b.InstrumentID == "fixture-01" && b.SessionDate.Format("2006-01-02") == o.TargetOpenAt.Format("2006-01-02") {
			b.AvailableAt = o.TargetOpenAt.Add(time.Hour)
		}
	}
	if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID}); e != nil {
		t.Fatal(e)
	}
	if _, e = h.orders.GetFill(ctx, h.scope, h.account.ID, o.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
}
func TestCancelBeforeOpenExactReleaseAndRetry(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	r := dto.CancelOrderRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "cancel/" + o.ID, Key: uuid(), ExpectedVersion: o.Version}, AccountID: h.account.ID, OrderID: o.ID}
	result, e := h.cancel.Handle(ctx, r)
	if e != nil || result.State != domain.OrderCancelled {
		t.Fatal(result, e)
	}
	again, e := h.cancel.Handle(ctx, r)
	if e != nil || again.Version != result.Version {
		t.Fatal(again, e)
	}
	account, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil || account.Balances.Available != 10000000 || account.Balances.Reserved != 0 {
		t.Fatal(account, e)
	}
	r.ExpectedVersion++
	if _, e = h.cancel.Handle(ctx, r); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal(e)
	}
	var count int
	e = h.pool.QueryRow(ctx, "select count(*) from investment.ledger_entries where account_id=$1 and event_key=$2", h.account.ID, "release/"+o.ID).Scan(&count)
	if e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
