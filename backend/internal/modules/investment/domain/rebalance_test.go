package domain

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestRebalanceSeededPortfoliosRespectCaps(t *testing.T) {
	rng := rand.New(rand.NewSource(7001))
	for run := 0; run < 120; run++ {
		p := riskFixture()
		p.Positions = nil
		p.Evaluation.Signals = nil
		stocks := Money(0)
		for n, i := range p.Snapshot.Instruments {
			qty := Quantity(rng.Intn(101))
			value, _ := GrossValue(100000000, qty)
			stocks += value
			p.Positions = append(p.Positions, Position{InstrumentID: i.ID, Industry: Industry(i.SIC), Quantity: qty, CostBasis: value})
			p.Evaluation.Signals = append(p.Evaluation.Signals, Signal{InstrumentID: i.ID, Industry: Industry(i.SIC), Score: 90, Rank: n + 1, Risk: RiskAssessment{Level: "low"}})
		}
		p.Account.Balances.Available = p.NAV - stocks
		plan, e := BuildRebalance(p)
		if e != nil {
			t.Fatal(run, e)
		}
		reversed := p
		reversed.Positions = append([]Position(nil), p.Positions...)
		reversed.Evaluation.Signals = append([]Signal(nil), p.Evaluation.Signals...)
		rng.Shuffle(len(reversed.Positions), func(i, j int) {
			reversed.Positions[i], reversed.Positions[j] = reversed.Positions[j], reversed.Positions[i]
		})
		rng.Shuffle(len(reversed.Evaluation.Signals), func(i, j int) {
			reversed.Evaluation.Signals[i], reversed.Evaluation.Signals[j] = reversed.Evaluation.Signals[j], reversed.Evaluation.Signals[i]
		})
		repeat, e := BuildRebalance(reversed)
		if e != nil || !reflect.DeepEqual(plan, repeat) {
			t.Fatal("unstable ordering", run, e)
		}
		holdings := map[string]Quantity{}
		for _, pos := range p.Positions {
			holdings[pos.InstrumentID] = pos.Quantity
		}
		available := p.Account.Balances.Available
		for _, o := range plan.Orders {
			if o.Side == "buy" {
				cost, e := purchaseCost(100000000, o.Quantity)
				if e != nil {
					t.Fatal(e)
				}
				available -= cost
				holdings[o.InstrumentID] += o.Quantity
			} else {
				holdings[o.InstrumentID] -= o.Quantity
			}
		}
		if available < 0 {
			t.Fatal("negative settled cash", run)
		}
		sector := map[string]Money{}
		total := Money(0)
		for _, i := range p.Snapshot.Instruments {
			qty := holdings[i.ID]
			if qty < 0 {
				t.Fatal("negative holdings")
			}
			value, _ := GrossValue(100000000, qty)
			if value > moneyLimit(p.NAV, p.Policy.SingleWeight) {
				t.Fatal("single", run, value)
			}
			sector[Industry(i.SIC)] += value
			total += value
		}
		for _, value := range sector {
			if value > moneyLimit(p.NAV, p.Policy.IndustryWeight) {
				t.Fatal("industry", run, value)
			}
		}
		if total > moneyLimit(p.NAV, p.Policy.StockWeight) {
			t.Fatal("stocks", run, total)
		}
	}
}
func TestRiskDoesNotInventPriceForHeldPosition(t *testing.T) {
	p := riskFixture()
	p.Positions = []Position{{InstrumentID: "absent", Quantity: 10, CostBasis: 100000}}
	_, e := BuildRebalance(p)
	if e == nil {
		t.Fatal("invented missing price")
	}
}
