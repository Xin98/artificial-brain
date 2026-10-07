package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

type orderHarness struct {
	pool     *pgxpool.Pool
	data     *fixture.Adapter
	scope    domain.Scope
	at       time.Time
	account  dto.AccountView
	place    command.PlaceOrderHandler
	cancel   command.CancelOrderHandler
	pause    command.ConfigureAutomationHandler
	execute  command.ExecuteOrdersHandler
	accounts *AccountStore
	orders   *OrderStore
}

func newOrderHarness(t *testing.T) *orderHarness {
	t.Helper()
	h := &orderHarness{pool: testPool(t), scope: domain.Scope{WorkspaceID: uuid(), OwnerUserID: uuid()}, at: time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)}
	h.data, _ = fixture.New()
	now := func() time.Time { return h.at }
	tx := database.NewTxRunner(h.pool)
	mutations := application.MutationExecutor{UOW: tx, Requests: NewRequestStore(h.pool)}
	h.accounts = NewAccountStore(h.pool)
	h.orders = NewOrderStore(h.pool)
	ledger := NewLedgerStore(h.pool)
	create := command.CreateAccountHandler{Mutations: mutations, Accounts: h.accounts, Catalog: NewCatalogStore(h.pool), Ledger: ledger, Data: h.data, Now: now, NewID: uuid}
	var e error
	h.account, e = create.Handle(context.Background(), dto.CreateAccountRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "accounts", Key: uuid()}, Mode: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	writer := command.ReservationWriter{Accounts: h.accounts, Orders: h.orders, Ledger: ledger, Now: now, NewID: uuid}
	h.place = command.PlaceOrderHandler{Mutations: mutations, Accounts: h.accounts, Orders: h.orders, Data: h.data, Reservations: writer, Now: now, NewID: uuid}
	h.cancel = command.CancelOrderHandler{Mutations: mutations, Accounts: h.accounts, Orders: h.orders, Ledger: ledger, Now: now, NewID: uuid}
	h.pause = command.ConfigureAutomationHandler{Mutations: mutations, Accounts: h.accounts, Catalog: NewCatalogStore(h.pool), Events: ledger, Data: h.data, Reservations: &writer, Now: now, NewID: uuid}
	h.execute = command.ExecuteOrdersHandler{UOW: tx, Accounts: h.accounts, Orders: h.orders, Ledger: ledger, Data: h.data, Now: now, NewID: uuid}
	return h
}
func (h *orderHarness) buy(t *testing.T, quantity string) dto.OrderView {
	t.Helper()
	a, e := h.accounts.Get(context.Background(), h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	out, e := h.place.Handle(context.Background(), dto.PlaceOrderRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "orders/" + a.ID, Key: uuid(), ExpectedVersion: a.Version}, AccountID: a.ID, InstrumentID: "fixture-01", Side: "buy", Quantity: quantity})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (h *orderHarness) stop(t *testing.T) {
	t.Helper()
	a, e := h.accounts.Get(context.Background(), h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.pause.Handle(context.Background(), dto.ConfigureAutomationRequest{Mutation: dto.Mutation{Scope: h.scope, Route: "automation/" + a.ID, Key: uuid(), ExpectedVersion: a.Version}, AccountID: a.ID, Enabled: false, Policy: a.Policy, StrategyVersionID: a.StrategyVersionID, UniverseVersionID: a.UniverseVersionID})
	if e != nil {
		t.Fatal(e)
	}
}
func TestPauseBeforeAndAfterOpenReservations(t *testing.T) {
	ctx := context.Background()
	before := newOrderHarness(t)
	o := before.buy(t, "10")
	before.stop(t)
	state, e := before.orders.GetOrder(ctx, before.scope, before.account.ID, o.ID)
	if e != nil || state.State != domain.OrderCancelled {
		t.Fatal(state, e)
	}
	a, e := before.accounts.Get(ctx, before.scope, before.account.ID)
	if e != nil || a.Balances.Reserved != 0 || a.Balances.Available != 10000000 {
		t.Fatal(a, e)
	}
	after := newOrderHarness(t)
	o = after.buy(t, "10")
	after.at = o.TargetOpenAt
	after.stop(t)
	state, e = after.orders.GetOrder(ctx, after.scope, after.account.ID, o.ID)
	if e != nil || state.State != domain.OrderAwaitingBar {
		t.Fatal(state, e)
	}
	a, e = after.accounts.Get(ctx, after.scope, after.account.ID)
	if e != nil || a.Balances.Reserved <= 0 || a.AutomationEnabled {
		t.Fatal(a, e)
	}
	after.at = o.TargetOpenAt.Add(9 * time.Hour)
	_, e = after.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: after.scope, AccountID: a.ID, SessionDate: o.TargetOpenAt})
	if e != nil {
		t.Fatal(e)
	}
	fill, e := after.orders.GetFill(ctx, after.scope, a.ID, o.ID)
	if e != nil || fill.Quantity != 9 {
		t.Fatal(fill, e)
	}
}
func TestSharedCapacityDatabaseAndTargetDayOnly(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	for n := range h.data.Snapshot.Bars {
		b := &h.data.Snapshot.Bars[n]
		if b.InstrumentID == "fixture-01" && b.SessionDate.Before(h.at) {
			b.Volume = 1000
		}
	}
	one := h.buy(t, "7")
	two := h.buy(t, "7")
	h.at = one.TargetOpenAt.Add(9 * time.Hour)
	if _, e := h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID, SessionDate: one.TargetOpenAt}); e != nil {
		t.Fatal(e)
	}
	var total int64
	e := h.pool.QueryRow(ctx, "select sum(quantity) from investment.fills where account_id=$1", h.account.ID).Scan(&total)
	if e != nil || total != 10 {
		t.Fatal(total, e)
	}
	f1, e := h.orders.GetFill(ctx, h.scope, h.account.ID, one.ID)
	if e != nil {
		t.Fatal(e)
	}
	f2, e := h.orders.GetFill(ctx, h.scope, h.account.ID, two.ID)
	if e != nil || f1.Quantity+f2.Quantity != 10 {
		t.Fatal(f1, f2, e)
	}
	// A missing target day expires even when a later day has a perfectly valid opening bar.
	h = newOrderHarness(t)
	o := h.buy(t, "10")
	bars := []domain.Bar{}
	for _, b := range h.data.Snapshot.Bars {
		if b.InstrumentID != "fixture-01" || b.SessionDate.Format("2006-01-02") != o.TargetOpenAt.Format("2006-01-02") {
			bars = append(bars, b)
		}
	}
	h.data.Snapshot.Bars = bars
	h.at = o.ExpiresAt.Add(24 * time.Hour)
	if _, e = h.execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: h.scope, AccountID: h.account.ID, SessionDate: o.TargetOpenAt}); e != nil {
		t.Fatal(e)
	}
	state, e := h.orders.GetOrder(ctx, h.scope, h.account.ID, o.ID)
	if e != nil || state.State != domain.OrderExpired {
		t.Fatal(state, e)
	}
	if _, e = h.orders.GetFill(ctx, h.scope, h.account.ID, o.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil || a.Balances.Available != 10000000 || a.Balances.Reserved != 0 {
		t.Fatal(a, e)
	}
}
