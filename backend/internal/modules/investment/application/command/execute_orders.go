package command

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"sort"
	"time"
)

type ExecuteOrdersHandler struct {
	UOW      ports.UnitOfWork
	Accounts ports.AccountStore
	Orders   ports.OrderStore
	Ledger   ports.LedgerStore
	Data     ports.ResearchData
	Now      func() time.Time
	NewID    func() string
	Actions  ports.ReconciliationStore
}

func (h ExecuteOrdersHandler) Handle(ctx context.Context, r dto.ExecuteOrdersRequest) (dto.ExecutionResult, error) {
	var result dto.ExecutionResult
	e := h.UOW.Run(ctx, func(ctx context.Context) error { var e error; result, e = h.Apply(ctx, r); return e })
	return result, e
}

// Apply supports a larger account transaction (reconciliation and evaluation) without nesting transactions.
func (h ExecuteOrdersHandler) Apply(ctx context.Context, r dto.ExecuteOrdersRequest) (dto.ExecutionResult, error) {
	result := dto.ExecutionResult{AccountID: r.AccountID}
	account, e := h.Accounts.Lock(ctx, r.Scope, r.AccountID)
	if e != nil {
		return result, e
	}
	now := h.Now()
	snapshot, e := h.Data.Read(ctx, account.Mode, now)
	if e != nil {
		return result, e
	}
	if e = domain.ValidateAccountDataset(account, snapshot); e != nil {
		return result, e
	}
	orders, e := h.Orders.ListPending(ctx, r.Scope, account.ID)
	if e != nil {
		return result, e
	}
	sort.SliceStable(orders, func(i, j int) bool {
		if orders[i].Side != orders[j].Side {
			return orders[i].Side == "sell"
		}
		if orders[i].InstrumentID != orders[j].InstrumentID {
			return orders[i].InstrumentID < orders[j].InstrumentID
		}
		if !orders[i].CreatedAt.Equal(orders[j].CreatedAt) {
			return orders[i].CreatedAt.Before(orders[j].CreatedAt)
		}
		return orders[i].ID < orders[j].ID
	})
	changed := false
	for _, order := range orders {
		if now.Before(order.TargetOpenAt) || (!r.SessionDate.IsZero() && r.SessionDate.UTC().Format("2006-01-02") != order.TargetOpenAt.UTC().Format("2006-01-02")) {
			continue
		}
		original := order
		order = domain.AdvanceOrder(order, now)
		blockedReason := ""
		for _, action := range snapshot.Actions {
			if action.InstrumentID != order.InstrumentID || (!action.EffectiveAt.IsZero() && (action.EffectiveAt.Before(account.CreatedAt) || action.EffectiveAt.After(order.TargetOpenAt))) {
				continue
			}
			if _, e := domain.ApplyCorporateAction(account, nil, action, nil); e != nil {
				blockedReason = "corporate_action_incomplete"
				break
			}
			if h.Actions == nil {
				if action.Kind == "split" {
					blockedReason = "corporate_action_unreconciled"
					break
				}
				continue
			}
			recorded, e := h.Actions.RecordedAction(ctx, r.Scope, account.ID, action.ID)
			if e != nil && !errors.Is(e, domain.ErrNotFound) {
				return result, e
			}
			if e == nil && !domain.SameActionEconomics(recorded, action) {
				blockedReason = "corporate_action_revision_requires_review"
				break
			}
			if action.Kind == "split" && errors.Is(e, domain.ErrNotFound) {
				blockedReason = "corporate_action_unreconciled"
				break
			}
		}
		if blockedReason != "" {
			result.Blocked = append(result.Blocked, domain.Exclusion{InstrumentID: order.InstrumentID, Reason: blockedReason})
		}
		targetDate := order.TargetOpenAt.UTC().Format("2006-01-02")
		opening := map[string]domain.Price{}
		var bar *domain.Bar
		for n, b := range snapshot.Bars {
			if b.SessionDate.Format("2006-01-02") == targetDate && !b.AvailableAt.After(order.ExpiresAt) && b.Open > 0 && !b.NoOpeningTrade {
				opening[b.InstrumentID] = b.Open
				if b.InstrumentID == order.InstrumentID {
					bar = &snapshot.Bars[n]
				}
			}
		}
		book, e := h.Orders.LoadRiskBook(ctx, r.Scope, account.ID, order.TargetOpenAt)
		if e != nil {
			return result, e
		}
		var fillResult domain.FillResult
		if blockedReason != "" {
			bar = nil
		}
		if bar != nil {
			prior, e := domain.SelectSnapshot(snapshot, order.TargetOpenAt.Add(-time.Nanosecond))
			if e != nil {
				return result, e
			}
			for _, pos := range book.Positions {
				if pos.Quantity > 0 && opening[pos.InstrumentID] <= 0 {
					bar = nil
					break
				}
			}
			if bar != nil {
				nav, e := domain.ComputeNAV(account, book.Positions, prior, opening)
				if e != nil {
					return result, e
				}
				used, e := h.Orders.SessionCapacityUsed(ctx, r.Scope, account.ID, order.InstrumentID, order.TargetOpenAt)
				if e != nil {
					return result, e
				}
				limit, e := h.Orders.SessionCapacityLimit(ctx, r.Scope, account.ID, order.InstrumentID, order.TargetOpenAt)
				if e != nil {
					return result, e
				}
				remaining := limit - used
				if remaining < 0 {
					remaining = 0
				}
				fillResult, e = domain.MatchAtOpen(domain.MatchInput{Order: order, OpenPrice: bar.Open, AvailableCapacity: remaining, Portfolio: domain.PortfolioInput{Account: account, Positions: book.Positions, Orders: book.Orders, Snapshot: prior, NAV: nav, PeakNAV: book.PeakNAV, Policy: account.Policy, SessionTurnover: book.SessionTurnover, Prices: opening}, RecordedAt: now})
				if e != nil {
					return result, e
				}
			}
		}
		if bar == nil {
			if now.Before(order.ExpiresAt) {
				if original.State != order.State {
					if e = h.Orders.SaveOrder(ctx, r.Scope, account.ID, order, original.Version); e != nil {
						return result, e
					}
				}
				result.Awaiting++
				continue
			}
			order.State = domain.OrderExpired
			order.Reason = "expired_data_unavailable"
			if blockedReason != "" {
				order.Reason = "expired_corporate_action_unresolved"
			}
		} else if fillResult.Fill != nil {
			fill := *fillResult.Fill
			fill.ID = h.NewID()
			mutation, e := domain.ApplyFill(account, book.Positions, fill)
			if e != nil {
				return result, e
			}
			account = mutation.Account
			for n, p := range mutation.Positions {
				if p.Industry == "" {
					for _, i := range snapshot.Instruments {
						if i.ID == p.InstrumentID {
							mutation.Positions[n].Industry = domain.Industry(i.SIC)
						}
					}
				}
			}
			if e = h.Orders.InsertFill(ctx, r.Scope, account.ID, fill); e != nil {
				return result, e
			}
			if e = h.Orders.SavePositions(ctx, r.Scope, account.ID, mutation.Positions); e != nil {
				return result, e
			}
			if e = h.Ledger.InsertLedger(ctx, r.Scope, account.ID, mutation.Entries); e != nil {
				return result, e
			}
			order.State = domain.OrderFilled
			if fillResult.CancelledQuantity > 0 {
				order.State = domain.OrderPartial
			}
			order.Reason = fillResult.Reason
			result.Filled++
			changed = true
		} else {
			order.State = domain.OrderRejected
			order.Reason = fillResult.Reason
		}
		if fillResult.Fill == nil {
			release, e := domain.ReleaseOrder(account, book.Positions, original, order.Reason, now)
			if e != nil {
				return result, e
			}
			account = release.Account
			for n := range release.Entries {
				release.Entries[n].ID = h.NewID()
				release.Entries[n].EffectiveAt = order.TargetOpenAt
				if order.State == domain.OrderExpired {
					release.Entries[n].EffectiveAt = order.ExpiresAt
				}
			}
			if e = h.Orders.SavePositions(ctx, r.Scope, account.ID, release.Positions); e != nil {
				return result, e
			}
			if e = h.Ledger.InsertLedger(ctx, r.Scope, account.ID, release.Entries); e != nil {
				return result, e
			}
			changed = true
			if order.State == domain.OrderExpired {
				result.Expired++
			}
		}
		order.ReservedCash = 0
		order.ReservedQuantity = 0
		if e = h.Orders.SaveOrder(ctx, r.Scope, account.ID, order, original.Version); e != nil {
			return result, e
		}
	}
	if changed {
		if e = h.Accounts.Save(ctx, account, account.Version); e != nil {
			return result, e
		}
	}
	return result, nil
}
