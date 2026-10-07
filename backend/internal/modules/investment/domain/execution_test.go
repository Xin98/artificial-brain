package domain

import (
	"testing"
	"time"
)

func matchingInput() MatchInput {
	p := riskFixture()
	p.Account.ID = "account"
	p.Account.Balances = Balances{Available: 500000, Reserved: 600111}
	p.Orders = []Order{{ID: "current", InstrumentID: "a", Side: "buy", State: OrderAwaitingBar, Quantity: 10, ReservedCash: 100111}}
	return MatchInput{Order: Order{ID: "current", AccountID: "account", InstrumentID: "a", Side: "buy", State: OrderAwaitingBar, Quantity: 10, ReservedCash: 100111, Capacity: 10, TargetOpenAt: at("2026-10-07T13:30:00Z"), ExpiresAt: at("2026-10-08T20:00:00Z")}, OpenPrice: 100000000, AvailableCapacity: 10, Portfolio: p, RecordedAt: at("2026-10-07T21:00:00Z")}
}
func TestFillCostsAndGapUsesOnlyReservation(t *testing.T) {
	input := matchingInput()
	result, e := MatchAtOpen(input)
	if e != nil || result.Fill == nil {
		t.Fatal(result, e)
	}
	f := result.Fill
	if f.Price != 100100000 || f.Gross != 100100 || f.Fee != 11 || f.Quantity != 10 {
		t.Fatal(f)
	}
	input.OpenPrice = 120000000
	result, e = MatchAtOpen(input)
	if e != nil || result.Fill == nil {
		t.Fatal(result, e)
	}
	f = result.Fill
	if f.Quantity != 8 || f.Gross+f.Fee != 96106 || result.CancelledQuantity != 2 {
		t.Fatal(result)
	}
	f.ID = "fill-id"
	mutation, e := ApplyFill(input.Portfolio.Account, nil, *f)
	if e != nil || mutation.Account.Balances.Reserved != 500000 || mutation.Account.Balances.Available != 504005 || mutation.Positions[0].CostBasis != 96106 {
		t.Fatal(mutation, e)
	}
}
func TestSharedCapacityCannotBeSplitAround(t *testing.T) {
	before := at("2026-10-07T13:30:00Z")
	var bars []Bar
	for n := 0; n < 20; n++ {
		bars = append(bars, Bar{Volume: 1000, SessionDate: before.AddDate(0, 0, n-21), AvailableAt: before.AddDate(0, 0, n-20)})
	}
	bars = append(bars, Bar{Volume: 1000000000, SessionDate: before, AvailableAt: before.Add(8 * time.Hour)})
	capacity, e := PreviousVolumeCapacity(bars, before)
	if e != nil || capacity != 10 {
		t.Fatal(capacity, e)
	}
	input := matchingInput()
	input.AvailableCapacity = capacity
	first, e := MatchAtOpen(input)
	if e != nil || first.Fill == nil {
		t.Fatal(first, e)
	}
	input.Order.ID = "second"
	input.AvailableCapacity = capacity - first.Fill.Quantity
	second, e := MatchAtOpen(input)
	if e != nil || second.Fill != nil || second.CancelledQuantity != 10 {
		t.Fatal(second, e)
	}
}
func TestPauseAfterOpenCannotUndoAwaitingBar(t *testing.T) {
	o := matchingInput().Order
	o.State = OrderPending
	if !o.Cancellable(o.TargetOpenAt.Add(-time.Nanosecond)) {
		t.Fatal(o)
	}
	advanced := AdvanceOrder(o, o.TargetOpenAt)
	if advanced.State != OrderAwaitingBar || advanced.Cancellable(o.TargetOpenAt) {
		t.Fatal(advanced)
	}
}
func TestSellCostBasisFinalCentAndUnsettledCash(t *testing.T) {
	input := matchingInput()
	input.Order.Side = "sell"
	input.Order.Quantity = 1
	input.Order.ReservedCash = 0
	input.Order.ReservedQuantity = 1
	input.Order.Capacity = 10
	input.Portfolio.Positions = []Position{{InstrumentID: "a", Industry: "manufacturing", Quantity: 3, ReservedQuantity: 1, CostBasis: 100}}
	input.Portfolio.Orders = []Order{input.Order}
	input.Portfolio.Snapshot.Calendar.SettlementDays = []time.Time{at("2026-10-08T00:00:00Z")}
	result, e := MatchAtOpen(input)
	if e != nil || result.Fill == nil {
		t.Fatal(result, e)
	}
	result.Fill.ID = "sell-1"
	m, e := ApplyFill(input.Portfolio.Account, input.Portfolio.Positions, *result.Fill)
	if e != nil || m.Positions[0].CostBasis != 67 || m.Positions[0].Quantity != 2 || m.Positions[0].ReservedQuantity != 0 || m.Account.Balances.Unsettled != 9989 {
		t.Fatal(m, e)
	}
	input.Portfolio.Account = m.Account
	input.Portfolio.Positions = m.Positions
	input.Portfolio.Positions[0].ReservedQuantity = 2
	input.Order.Quantity = 2
	input.Order.ReservedQuantity = 2
	input.Portfolio.Orders = []Order{input.Order}
	result, e = MatchAtOpen(input)
	if e != nil {
		t.Fatal(e)
	}
	result.Fill.ID = "sell-2"
	m, e = ApplyFill(input.Portfolio.Account, input.Portfolio.Positions, *result.Fill)
	if e != nil || m.Positions[0].CostBasis != 0 || m.Positions[0].Quantity != 0 {
		t.Fatal(m, e)
	}
}
