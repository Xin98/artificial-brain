package domain

import (
	"math/big"
	"sort"
	"time"
)

const ExecutionModel = "next-session-open-v1"

type MatchInput struct {
	Order             Order
	OpenPrice         Price
	AvailableCapacity Quantity
	Portfolio         PortfolioInput
	RecordedAt        time.Time
}
type Fill struct {
	ID, AccountID, OrderID, InstrumentID, Side, Reason string
	Quantity                                           Quantity
	Price                                              Price
	Gross, Fee, ReservedCash                           Money
	ReservedQuantity                                   Quantity
	EffectiveAt, RecordedAt, SettlesAt                 time.Time
}
type FillResult struct {
	Fill              *Fill
	CancelledQuantity Quantity
	Reason            string
}

func AdvanceOrder(o Order, now time.Time) Order {
	if o.State == OrderPending && !now.Before(o.TargetOpenAt) {
		o.State = OrderAwaitingBar
	}
	return o
}
func SlippedPrice(open Price, side string) (Price, error) {
	switch side {
	case "buy":
		return scalePrice(open, 10010, 10000)
	case "sell":
		return scalePrice(open, 9990, 10000)
	}
	return 0, ErrInvalidInput
}
func ReservationCost(close Price, qty Quantity) (Money, error) {
	price, e := SlippedPrice(close, "buy")
	if e != nil {
		return 0, e
	}
	return purchaseCost(price, qty)
}
func PreviousVolumeCapacity(bars []Bar, before time.Time) (Quantity, error) {
	filtered := []Bar{}
	for _, b := range bars {
		if b.AvailableAt.Before(before) && b.SessionDate.Before(dateUTC(before)) {
			filtered = append(filtered, b)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].SessionDate.Before(filtered[j].SessionDate) })
	if len(filtered) < 20 {
		return 0, ErrDataStale
	}
	filtered = filtered[len(filtered)-20:]
	sum := new(big.Int)
	seen := map[string]bool{}
	for _, b := range filtered {
		key := b.SessionDate.Format("2006-01-02")
		if seen[key] || b.Volume < 0 {
			return 0, ErrInvalidInput
		}
		seen[key] = true
		sum.Add(sum, big.NewInt(int64(b.Volume)))
	}
	sum.Quo(sum, big.NewInt(2000))
	if !sum.IsInt64() {
		return 0, ErrOverflow
	}
	return Quantity(sum.Int64()), nil
}
func MatchAtOpen(in MatchInput) (FillResult, error) {
	out := FillResult{CancelledQuantity: in.Order.Quantity}
	o := AdvanceOrder(in.Order, in.RecordedAt)
	if o.State != OrderAwaitingBar || in.RecordedAt.Before(o.TargetOpenAt) || o.Quantity <= 0 || o.Capacity < 0 || in.AvailableCapacity < 0 || o.ReservedCash < 0 || o.ReservedQuantity < 0 {
		return out, ErrInvalidInput
	}
	price, e := SlippedPrice(in.OpenPrice, o.Side)
	if e != nil {
		return out, e
	}
	budget := o.ReservedCash
	orders := append([]Order(nil), in.Portfolio.Orders...)
	found := false
	for _, existing := range orders {
		if existing.ID == o.ID {
			found = true
		}
	}
	if !found {
		orders = append(orders, o)
	}
	risk, e := ValidateOrder(OrderRiskInput{Account: in.Portfolio.Account, Positions: in.Portfolio.Positions, Orders: orders, Snapshot: in.Portfolio.Snapshot, InstrumentID: o.InstrumentID, Side: o.Side, Reason: o.Reason, ExcludeOrderID: o.ID, Quantity: o.Quantity, Price: price, NAV: in.Portfolio.NAV, PeakNAV: in.Portfolio.PeakNAV, Policy: in.Portfolio.Policy, SessionTurnover: in.Portfolio.SessionTurnover, ReservationBudget: &budget, Prices: in.Portfolio.Prices, TargetOpenAt: o.TargetOpenAt})
	if e != nil {
		return out, e
	}
	qty := o.Quantity
	for _, max := range []Quantity{risk.MaxQuantity, o.Capacity, in.AvailableCapacity} {
		if qty > max {
			qty = max
		}
	}
	if o.Side == "sell" && qty > o.ReservedQuantity {
		qty = o.ReservedQuantity
	}
	if qty <= 0 {
		out.Reason = risk.ReasonCode
		if out.Reason == "" {
			out.Reason = "capacity_exhausted"
		}
		return out, nil
	}
	gross, e := GrossValue(price, qty)
	if e != nil {
		return out, e
	}
	fee, e := TradeFee(gross)
	if e != nil {
		return out, e
	}
	if o.Side == "sell" && gross < fee {
		out.Reason = "gross_below_fee"
		return out, nil
	}
	fill := Fill{AccountID: o.AccountID, OrderID: o.ID, InstrumentID: o.InstrumentID, Side: o.Side, Reason: o.Reason, Quantity: qty, Price: price, Gross: gross, Fee: fee, ReservedCash: o.ReservedCash, ReservedQuantity: o.ReservedQuantity, EffectiveAt: o.TargetOpenAt, RecordedAt: in.RecordedAt}
	if o.Side == "sell" {
		fill.SettlesAt, e = in.Portfolio.Snapshot.Calendar.SettlementStart(o.TargetOpenAt)
		if e != nil {
			return out, e
		}
	}
	out.Fill = &fill
	out.CancelledQuantity = o.Quantity - qty
	if out.CancelledQuantity > 0 {
		out.Reason = "unfilled_remainder_cancelled"
	}
	return out, nil
}
