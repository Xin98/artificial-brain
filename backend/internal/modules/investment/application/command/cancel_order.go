package command

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type CancelOrderHandler struct {
	Mutations application.MutationExecutor
	Accounts  ports.AccountStore
	Orders    ports.OrderStore
	Ledger    ports.LedgerStore
	Now       func() time.Time
	NewID     func() string
}

func (h CancelOrderHandler) Handle(ctx context.Context, r dto.CancelOrderRequest) (dto.OrderView, error) {
	if r.ExpectedVersion < 1 {
		return dto.OrderView{}, domain.ErrInvalidInput
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.OrderView, error) {
		a, e := h.Accounts.Lock(ctx, r.Scope, r.AccountID)
		if e != nil {
			return dto.OrderView{}, e
		}
		o, e := h.Orders.LockOrder(ctx, r.Scope, a.ID, r.OrderID)
		if e != nil {
			return dto.OrderView{}, e
		}
		now := h.Now()
		advanced := domain.AdvanceOrder(o, now)
		if advanced.State != o.State {
			advanced.Reason = "awaiting_daily_bar"
			if e = h.Orders.SaveOrder(ctx, r.Scope, a.ID, advanced, o.Version); e != nil {
				return dto.OrderView{}, fmt.Errorf("advance open order: %w", e)
			}
			// This clock transition is intentionally committed alongside the refusal audit.
			return dto.OrderView{}, domain.ErrOrderNotCancellable
		}
		if !o.Cancellable(now) {
			return dto.OrderView{}, domain.ErrOrderNotCancellable
		}
		if o.Version != r.ExpectedVersion {
			return dto.OrderView{}, domain.ErrVersionConflict
		}
		positions, e := h.Orders.LoadPositions(ctx, r.Scope, a.ID)
		if e != nil {
			return dto.OrderView{}, e
		}
		released, e := domain.ReleaseOrder(a, positions, o, "user_cancelled", now)
		if e != nil {
			return dto.OrderView{}, e
		}
		for n := range released.Entries {
			released.Entries[n].ID = h.NewID()
		}
		o.State = domain.OrderCancelled
		o.Reason = "user_cancelled"
		o.ReservedCash = 0
		o.ReservedQuantity = 0
		if e = h.Orders.SavePositions(ctx, r.Scope, a.ID, released.Positions); e != nil {
			return dto.OrderView{}, fmt.Errorf("save cancellation positions: %w", e)
		}
		if e = h.Ledger.InsertLedger(ctx, r.Scope, a.ID, released.Entries); e != nil {
			return dto.OrderView{}, fmt.Errorf("record cancellation: %w", e)
		}
		if e = h.Orders.SaveOrder(ctx, r.Scope, a.ID, o, o.Version); e != nil {
			return dto.OrderView{}, fmt.Errorf("save cancellation order: %w", e)
		}
		if e = h.Accounts.Save(ctx, released.Account, a.Version); e != nil {
			return dto.OrderView{}, fmt.Errorf("save cancellation account: %w", e)
		}
		o.Version++
		return dto.ViewOrder(o), nil
	})
}
