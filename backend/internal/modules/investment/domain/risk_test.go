package domain

import (
	"testing"
	"time"
)

func riskFixture() PortfolioInput {
	s := Snapshot{AsOf: at("2026-10-06T21:00:00Z"), Mode: "fixture", DatasetVersion: "fixture/synthetic/test", Calendar: Calendar{CoverageStart: at("2026-01-01T00:00:00Z"), CoverageEnd: at("2026-12-31T23:59:59Z"), Sessions: []Session{{Date: at("2026-10-06T00:00:00Z"), OpenAt: at("2026-10-06T13:30:00Z"), CloseAt: at("2026-10-06T20:00:00Z")}}}}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		sic := "3571"
		if id > "c" {
			sic = "7372"
		}
		s.Instruments = append(s.Instruments, Instrument{ID: id, Kind: "stock", Tradable: true, SIC: sic})
		s.Bars = append(s.Bars, Bar{InstrumentID: id, SessionDate: at("2026-10-06T00:00:00Z"), AvailableAt: s.AsOf, Close: 100000000})
	}
	return PortfolioInput{Account: Account{Balances: Balances{Available: 10000000}, InitialCash: 10000000, Policy: DefaultRiskPolicy()}, Snapshot: s, Policy: DefaultRiskPolicy(), NAV: 10000000, PeakNAV: 10000000, Evaluation: Evaluation{State: "completed", Signals: []Signal{{InstrumentID: "a", Score: 90, Rank: 1, Industry: "manufacturing", Risk: RiskAssessment{Level: "low"}}, {InstrumentID: "b", Score: 80, Rank: 2, Industry: "manufacturing", Risk: RiskAssessment{Level: "low"}}, {InstrumentID: "c", Score: 70, Rank: 3, Industry: "manufacturing", Risk: RiskAssessment{Level: "high"}}, {InstrumentID: "d", Score: 49.9, Rank: 4, Industry: "services", Risk: RiskAssessment{Level: "low"}}}}}
}
func TestRiskCapsBothManualAndAutomatic(t *testing.T) {
	p := riskFixture()
	for _, automatic := range []bool{false, true} {
		decision, e := ValidateOrder(OrderRiskInput{Account: p.Account, Snapshot: p.Snapshot, Evaluation: p.Evaluation, InstrumentID: "a", Side: "buy", Quantity: 101, Price: 100000000, NAV: p.NAV, Policy: p.Policy, Automatic: automatic})
		if e != nil || decision.Allowed || decision.MaxQuantity != 100 {
			t.Fatal(decision, e)
		}
	}
	p.Account.Balances.Available = 0
	p.Account.Balances.Unsettled = 10000000
	d, e := ValidateOrder(OrderRiskInput{Account: p.Account, Snapshot: p.Snapshot, InstrumentID: "a", Side: "buy", Quantity: 1, Price: 100000000, NAV: p.NAV, Policy: p.Policy})
	if e != nil || d.Allowed {
		t.Fatal(d, e)
	}
}
func TestRebalancePriorityAndBand(t *testing.T) {
	p := riskFixture()
	p.Positions = []Position{{InstrumentID: "a", Industry: "manufacturing", Quantity: 50, CostBasis: 600000}, {InstrumentID: "b", Industry: "manufacturing", Quantity: 85, CostBasis: 850000}, {InstrumentID: "d", Industry: "services", Quantity: 20, CostBasis: 200000}}
	p.Evaluation.Signals[1].Score = 60
	plan, e := BuildRebalance(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(plan.Orders) != 2 || plan.Orders[0].InstrumentID != "a" || plan.Orders[0].Reason != "stop_loss" || plan.Orders[0].Side != "sell" || plan.Orders[1].InstrumentID != "d" {
		t.Fatal(plan)
	}
	for _, o := range plan.Orders {
		if o.InstrumentID == "b" || o.InstrumentID == "c" {
			t.Fatal(o)
		}
	}
	p = riskFixture()
	p.Positions = []Position{{InstrumentID: "a", Industry: "manufacturing", Quantity: 85, CostBasis: 850000}}
	plan, e = BuildRebalance(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, o := range plan.Orders {
		if o.InstrumentID == "a" {
			t.Fatal("within two point band", o)
		}
	}
}
func TestDrawdownPauseKeepsPositions(t *testing.T) {
	p := riskFixture()
	p.NAV = 8500000
	p.Positions = []Position{{InstrumentID: "a", Quantity: 10, CostBasis: 100000}}
	plan, e := BuildRebalance(p)
	if e != nil || !plan.Pause || len(plan.Orders) != 0 {
		t.Fatal(plan, e)
	}
}
func TestRebalanceHonorsBoundStrategyParameters(t *testing.T) {
	p := riskFixture()
	params := DefaultStrategyParameters()
	params.EntryScore = 95
	p.Parameters = &params
	result, e := BuildRebalance(p)
	if e != nil || len(result.Orders) != 0 {
		t.Fatal(result, e)
	}
}
func TestRiskCountsPendingBuysAndOrdinaryTurnover(t *testing.T) {
	p := riskFixture()
	p.Orders = []Order{{ID: "old", InstrumentID: "a", Side: "buy", State: "pending", Quantity: 90, ReservedCash: 900100}}
	d, e := ValidateOrder(OrderRiskInput{Account: p.Account, Snapshot: p.Snapshot, Orders: p.Orders, InstrumentID: "a", Side: "buy", Quantity: 20, Price: 100000000, NAV: p.NAV, Policy: p.Policy})
	if e != nil || d.Allowed || d.MaxQuantity > 9 {
		t.Fatal(d, e)
	}
	p.Positions = []Position{{InstrumentID: "a", Quantity: 100, CostBasis: 1000000}}
	d, e = ValidateOrder(OrderRiskInput{Account: p.Account, Positions: p.Positions, Snapshot: p.Snapshot, InstrumentID: "a", Side: "sell", Quantity: 100, Price: 100000000, NAV: p.NAV, Policy: p.Policy, SessionTurnover: 1500000})
	if e != nil || d.Allowed || d.MaxQuantity != 50 {
		t.Fatal(d, e)
	}
	d, e = ValidateOrder(OrderRiskInput{Account: p.Account, Positions: p.Positions, Snapshot: p.Snapshot, InstrumentID: "a", Side: "sell", Quantity: 100, Price: 100000000, NAV: p.NAV, Policy: p.Policy, SessionTurnover: 1500000, Reason: "stop_loss"})
	if e != nil || !d.Allowed {
		t.Fatal(d, e)
	}
	_ = time.UTC
}
