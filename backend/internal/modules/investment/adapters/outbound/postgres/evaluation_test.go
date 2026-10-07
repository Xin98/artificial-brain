package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"reflect"
	"sync"
	"testing"
	"time"
)

func (h *orderHarness) evaluator() command.EvaluateAccountHandler {
	return command.EvaluateAccountHandler{UOW: database.NewTxRunner(h.pool), Accounts: h.accounts, Catalog: NewCatalogStore(h.pool), Orders: h.orders, Ledger: NewLedgerStore(h.pool), Runs: NewRunStore(h.pool), Snapshots: NewSnapshotStore(h.pool), Data: h.data, Reservations: h.place.Reservations, Now: func() time.Time { return h.at }, NewID: uuid}
}
func TestDailyEvaluationFreezesInputAcrossRetries(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.AutomationEnabled = true
	if e = h.accounts.Save(ctx, a, a.Version); e != nil {
		t.Fatal(e)
	}
	evaluator := h.evaluator()
	req := dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic", SessionDate: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
	// Freeze before calculation, then change source facts; retries must retain the original snapshot.
	prepared, e := evaluator.Prepare(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	old, e := NewSnapshotStore(h.pool).Get(ctx, a.DatasetVersion, prepared.SnapshotID)
	if e != nil {
		t.Fatal(e)
	}
	for n := range h.data.Snapshot.Facts {
		f := &h.data.Snapshot.Facts[n]
		if f.Concept == "NetIncome" {
			f.Value = "999999999999"
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := evaluator.Handle(ctx, req); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	again, e := evaluator.Prepare(ctx, req)
	if e != nil || again.ID != prepared.ID || again.SnapshotID != prepared.SnapshotID {
		t.Fatal(again, e)
	}
	frozen, e := NewSnapshotStore(h.pool).Get(ctx, a.DatasetVersion, prepared.SnapshotID)
	if e != nil || !reflect.DeepEqual(old, frozen) {
		t.Fatal("snapshot changed", e)
	}
	var runs, orders int
	h.pool.QueryRow(ctx, "select count(*) from investment.evaluation_runs where account_id=$1", a.ID).Scan(&runs)
	h.pool.QueryRow(ctx, "select count(*) from investment.orders where account_id=$1", a.ID).Scan(&orders)
	if runs != 1 || orders == 0 {
		t.Fatal(runs, orders)
	}
}
func TestMissedOpenDoesNotBackdateNewOrders(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.AutomationEnabled = true
	if e = h.accounts.Save(ctx, a, a.Version); e != nil {
		t.Fatal(e)
	}
	h.at = time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)
	result, e := h.evaluator().Handle(ctx, dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic", SessionDate: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)})
	if e != nil || result.IssuedOrders || result.Reason != "missed_open_research_only" {
		t.Fatal(result, e)
	}
	var count int
	h.pool.QueryRow(ctx, "select count(*) from investment.orders where account_id=$1", a.ID).Scan(&count)
	if count != 0 {
		t.Fatal(count)
	}
}
func TestPendingPreviousFillBlocksRebuy(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	o := h.buy(t, "10")
	h.at = o.TargetOpenAt.Add(9 * time.Hour)
	a, e := h.accounts.Get(ctx, h.scope, h.account.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.AutomationEnabled = true
	if e = h.accounts.Save(ctx, a, a.Version); e != nil {
		t.Fatal(e)
	}
	result, e := h.evaluator().Handle(ctx, dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "automatic", SessionDate: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)})
	if e != nil || result.IssuedOrders || result.Reason != "unresolved_effective_orders" {
		t.Fatal(result, e)
	}
	var count int
	h.pool.QueryRow(ctx, "select count(*) from investment.orders where account_id=$1", a.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
