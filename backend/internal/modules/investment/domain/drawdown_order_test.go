package domain

import "testing"

func TestDrawdownBlocksManualBuyButAllowsRiskReduction(t *testing.T) {
	p := riskFixture()
	input := OrderRiskInput{Account: p.Account, Snapshot: p.Snapshot, InstrumentID: "a", Side: "buy", Quantity: 1, Price: 100000000, NAV: 8500000, Policy: p.Policy}
	got, err := ValidateOrder(input)
	if err != nil || got.Allowed || got.ReasonCode != "drawdown_pause" {
		t.Fatal(got, err)
	}
	input.Side = "sell"
	input.Reason = "stop_loss"
	input.Positions = []Position{{InstrumentID: "a", Quantity: 10}}
	got, err = ValidateOrder(input)
	if err != nil || !got.Allowed {
		t.Fatal(got, err)
	}
}
