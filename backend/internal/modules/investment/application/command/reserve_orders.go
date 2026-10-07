package command

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type ReservationWriter struct {
	Accounts ports.AccountStore
	Orders   ports.OrderStore
	Ledger   ports.LedgerStore
	Now      func() time.Time
	NewID    func() string
}

// Apply participates in the caller's account-locked transaction. It never starts another transaction.
func (w ReservationWriter) Apply(ctx context.Context, scope domain.Scope, a domain.Account, orders []domain.Order) (domain.Account, error) {
	if scope != a.Scope {
		return a, domain.ErrNotFound
	}
	if len(orders) == 0 {
		return a, nil
	}
	positions, e := w.Orders.LoadPositions(ctx, scope, a.ID)
	if e != nil {
		return a, e
	}
	result, e := domain.ReserveOrders(a, positions, orders)
	if e != nil {
		return a, e
	}
	for n := range result.Entries {
		result.Entries[n].ID = w.NewID()
	}
	for _, o := range orders {
		if e = w.Orders.InsertOrder(ctx, scope, a.ID, o); e != nil {
			return a, fmt.Errorf("insert reservation order: %w", e)
		}
	}
	if e = w.Orders.SavePositions(ctx, scope, a.ID, result.Positions); e != nil {
		return a, fmt.Errorf("save reserved positions: %w", e)
	}
	if e = w.Ledger.InsertLedger(ctx, scope, a.ID, result.Entries); e != nil {
		return a, fmt.Errorf("record reservations: %w", e)
	}
	if e = w.Accounts.Save(ctx, result.Account, a.Version); e != nil {
		return a, fmt.Errorf("save reservations: %w", e)
	}
	result.Account.Version++
	return result.Account, nil
}

// CancelBeforeOpen advances effective orders before cancelling; a late pause cannot undo the open.
func (w ReservationWriter) CancelBeforeOpen(ctx context.Context, a domain.Account, reason string, buysOnly bool) (domain.Account, error) {
	orders, e := w.Orders.ListPending(ctx, a.Scope, a.ID)
	if e != nil {
		return a, e
	}
	positions, e := w.Orders.LoadPositions(ctx, a.Scope, a.ID)
	if e != nil {
		return a, e
	}
	now := w.Now()
	for _, o := range orders {
		advanced := domain.AdvanceOrder(o, now)
		if advanced.State != o.State {
			advanced.Reason = "awaiting_daily_bar"
			if e = w.Orders.SaveOrder(ctx, a.Scope, a.ID, advanced, o.Version); e != nil {
				return a, fmt.Errorf("advance effective order: %w", e)
			}
			continue
		}
		if !o.Cancellable(now) || (buysOnly && o.Side != "buy") {
			continue
		}
		release, e := domain.ReleaseOrder(a, positions, o, reason, now)
		if e != nil {
			return a, fmt.Errorf("release pending order: %w", e)
		}
		a = release.Account
		positions = release.Positions
		for n := range release.Entries {
			release.Entries[n].ID = w.NewID()
		}
		o.State = domain.OrderCancelled
		o.Reason = reason
		o.ReservedCash = 0
		o.ReservedQuantity = 0
		if e = w.Orders.SaveOrder(ctx, a.Scope, a.ID, o, o.Version); e != nil {
			return a, fmt.Errorf("cancel pending order: %w", e)
		}
		if e = w.Ledger.InsertLedger(ctx, a.Scope, a.ID, release.Entries); e != nil {
			return a, fmt.Errorf("record released order: %w", e)
		}
	}
	if e = w.Orders.SavePositions(ctx, a.Scope, a.ID, positions); e != nil {
		return a, fmt.Errorf("save released positions: %w", e)
	}
	return a, nil
}
