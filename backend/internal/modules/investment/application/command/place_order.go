package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type PlaceOrderHandler struct {
	Mutations    application.MutationExecutor
	Accounts     ports.AccountStore
	Orders       ports.OrderStore
	Data         ports.ResearchData
	Reservations ReservationWriter
	Now          func() time.Time
	NewID        func() string
}

func (h PlaceOrderHandler) Handle(ctx context.Context, r dto.PlaceOrderRequest) (dto.OrderView, error) {
	qty, e := domain.ParseQuantity(r.Quantity)
	if e != nil || r.ExpectedVersion < 1 || (r.Side != "buy" && r.Side != "sell") {
		return dto.OrderView{}, domain.ErrInvalidInput
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.OrderView, error) {
		a, e := h.Accounts.Lock(ctx, r.Scope, r.AccountID)
		if e != nil {
			return dto.OrderView{}, e
		}
		if a.Version != r.ExpectedVersion {
			return dto.OrderView{}, domain.ErrVersionConflict
		}
		now := h.Now()
		s, e := h.Data.Read(ctx, a.Mode, now)
		if e != nil {
			return dto.OrderView{}, e
		}
		if e = domain.ValidateAccountDataset(a, s); e != nil {
			return dto.OrderView{}, e
		}
		latest, e := s.Calendar.LatestCompleted(now)
		if e != nil {
			return dto.OrderView{}, e
		}
		target, e := s.Calendar.NextSession(now)
		if e != nil {
			return dto.OrderView{}, e
		}
		following, e := s.Calendar.NextSession(target.CloseAt)
		if e != nil {
			return dto.OrderView{}, e
		}
		if _, e = s.Calendar.NextSettlement(target.OpenAt); e != nil {
			return dto.OrderView{}, domain.ErrDataNotConfigured
		}
		var bars []domain.Bar
		close := domain.Price(0)
		for _, b := range s.Bars {
			if b.InstrumentID == r.InstrumentID {
				bars = append(bars, b)
				if b.SessionDate.Equal(latest.Date) {
					close = b.Close
				}
			}
		}
		if close <= 0 {
			return dto.OrderView{}, domain.ErrDataStale
		}
		capacity, e := domain.PreviousVolumeCapacity(bars, target.OpenAt)
		if e != nil {
			return dto.OrderView{}, e
		}
		if capacity <= 0 {
			return dto.OrderView{}, domain.ErrRiskLimit
		}
		book, e := h.Orders.LoadRiskBook(ctx, r.Scope, a.ID, target.Date)
		if e != nil {
			return dto.OrderView{}, e
		}
		nav, e := domain.ComputeNAV(a, book.Positions, s, nil)
		if e != nil {
			return dto.OrderView{}, e
		}
		price, e := domain.SlippedPrice(close, r.Side)
		if e != nil {
			return dto.OrderView{}, e
		}
		risk, e := domain.ValidateOrder(domain.OrderRiskInput{Account: a, Positions: book.Positions, Orders: book.Orders, Snapshot: s, InstrumentID: r.InstrumentID, Side: r.Side, Quantity: qty, Price: price, NAV: nav, PeakNAV: book.PeakNAV, Policy: a.Policy, SessionTurnover: book.SessionTurnover, TargetOpenAt: target.OpenAt})
		if e != nil {
			return dto.OrderView{}, e
		}
		if !risk.Allowed {
			if risk.ReasonCode == "insufficient_settled_cash" {
				return dto.OrderView{}, domain.ErrInsufficientCash
			}
			return dto.OrderView{}, domain.ErrRiskLimit
		}
		o := domain.Order{ID: h.NewID(), AccountID: a.ID, InstrumentID: r.InstrumentID, Side: r.Side, State: domain.OrderPending, Origin: "manual", Quantity: qty, Capacity: capacity, TargetOpenAt: target.OpenAt, ExpiresAt: following.CloseAt, CreatedAt: now, Version: 1}
		if r.Side == "buy" {
			o.ReservedCash, e = domain.ReservationCost(close, qty)
			if e != nil {
				return dto.OrderView{}, e
			}
		} else {
			o.ReservedQuantity = qty
		}
		if _, e = h.Reservations.Apply(ctx, r.Scope, a, []domain.Order{o}); e != nil {
			return dto.OrderView{}, e
		}
		return dto.ViewOrder(o), nil
	})
}
