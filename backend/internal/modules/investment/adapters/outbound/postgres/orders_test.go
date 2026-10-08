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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFillTransactionCrashAndRace(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	data, _ := fixture.New()
	scope := domain.Scope{WorkspaceID: uuid(), OwnerUserID: uuid()}
	var clock atomic.Int64
	clock.Store(time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC).UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	tx := database.NewTxRunner(p)
	mutations := application.MutationExecutor{UOW: tx, Requests: NewRequestStore(p)}
	accounts := NewAccountStore(p)
	orders := NewOrderStore(p)
	ledger := NewLedgerStore(p)
	create := command.CreateAccountHandler{Mutations: mutations, Accounts: accounts, Catalog: NewCatalogStore(p), Ledger: ledger, Data: data, Now: now, NewID: uuid}
	a, e := create.Handle(ctx, dto.CreateAccountRequest{Mutation: dto.Mutation{Scope: scope, Route: "accounts", Key: uuid()}, Mode: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	writer := command.ReservationWriter{Accounts: accounts, Orders: orders, Ledger: ledger, Now: now, NewID: uuid}
	place := command.PlaceOrderHandler{Mutations: mutations, Accounts: accounts, Orders: orders, Data: data, Reservations: writer, Now: now, NewID: uuid}
	r := dto.PlaceOrderRequest{Mutation: dto.Mutation{Scope: scope, Route: "orders/" + a.ID, Key: uuid(), ExpectedVersion: a.Version}, AccountID: a.ID, InstrumentID: "fixture-01", Side: "buy", Quantity: "10"}
	o, e := place.Handle(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	same, e := place.Handle(ctx, r)
	if e != nil || same.ID != o.ID {
		t.Fatal(same, e)
	}
	cancel := command.CancelOrderHandler{Mutations: mutations, Accounts: accounts, Orders: orders, Ledger: ledger, Now: now, NewID: uuid}
	clock.Store(o.TargetOpenAt.UnixNano())
	_, e = cancel.Handle(ctx, dto.CancelOrderRequest{Mutation: dto.Mutation{Scope: scope, Route: "cancel/" + o.ID, Key: uuid(), ExpectedVersion: o.Version}, AccountID: a.ID, OrderID: o.ID})
	if !errors.Is(e, domain.ErrOrderNotCancellable) {
		t.Fatal(e)
	}
	state, e := orders.GetOrder(ctx, scope, a.ID, o.ID)
	if e != nil || state.State != domain.OrderAwaitingBar {
		t.Fatal(state, e)
	}
	clock.Store(o.TargetOpenAt.Add(9 * time.Hour).UnixNano())
	execute := command.ExecuteOrdersHandler{UOW: tx, Accounts: accounts, Orders: orders, Ledger: ledger, Data: data, Now: now, NewID: uuid}
	job := dto.ExecuteOrdersRequest{Scope: scope, AccountID: a.ID, SessionDate: o.TargetOpenAt}
	// Abort after all economic writes; retry must start from the unchanged reservation.
	sentinel := errors.New("fixture crash before commit")
	e = tx.Run(ctx, func(ctx context.Context) error {
		if _, e := execute.Apply(ctx, job); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	var count int
	p.QueryRow(ctx, "select count(*) from investment.fills where account_id=$1", a.ID).Scan(&count)
	if count != 0 {
		t.Fatal(count)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := execute.Handle(ctx, job); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	p.QueryRow(ctx, "select count(*) from investment.fills where account_id=$1", a.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	held, e := orders.LoadPositions(ctx, scope, a.ID)
	// The fixed October 7 opening is above the prior close: the original reservation buys 9 whole shares.
	if e != nil || len(held) != 1 || held[0].Quantity != 9 || held[0].ReservedQuantity != 0 || held[0].CostBasis != 25283 {
		t.Fatal(held, e)
	}
	saved, e := accounts.Get(ctx, scope, a.ID)
	if e != nil || saved.Balances.Reserved != 0 {
		t.Fatal(saved, e)
	}
	if _, e = execute.Handle(ctx, job); e != nil {
		t.Fatal(e)
	}
	p.QueryRow(ctx, "select count(*) from investment.fills where account_id=$1", a.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	other := domain.Scope{WorkspaceID: scope.WorkspaceID, OwnerUserID: uuid()}
	if _, e = orders.GetOrder(ctx, other, a.ID, o.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
}
