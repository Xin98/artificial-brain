package domain

import (
	"errors"
	"testing"
	"time"
)

func TestSettlementSkipsNonSettlementDay(t *testing.T) {
	a := Account{ID: "a", Balances: Balances{Unsettled: 100000}}
	c := Calendar{CoverageStart: at("2026-01-01T00:00:00Z"), CoverageEnd: at("2026-12-31T23:59:59Z"), SettlementDays: []time.Time{at("2026-10-13T00:00:00Z")}}
	entry := LedgerEntry{AccountID: a.ID, EventKey: "fill/sell", Kind: "sell_fill", Delta: Balances{Unsettled: 100000}, EffectiveAt: at("2026-10-09T13:30:00Z")}
	before, e := SettleReceivables(a, []LedgerEntry{entry}, c, at("2026-10-12T20:00:00Z"))
	if e != nil || before.Account.Balances.Available != 0 {
		t.Fatal(before, e)
	}
	early, e := SettleReceivables(a, []LedgerEntry{entry}, c, at("2026-10-13T03:59:59Z"))
	if e != nil || early.Account.Balances.Available != 0 {
		t.Fatal(early, e)
	}
	settled, e := SettleReceivables(a, []LedgerEntry{entry}, c, at("2026-10-13T04:00:00Z"))
	if e != nil || settled.Account.Balances.Available != 100000 || settled.Account.Balances.Unsettled != 0 || len(settled.Entries) != 1 || !settled.Entries[0].EffectiveAt.Equal(at("2026-10-13T04:00:00Z")) {
		t.Fatal(settled, e)
	}
}
func TestSplitPreservesCost(t *testing.T) {
	a := Account{ID: "a"}
	p := []Position{{InstrumentID: "x", Quantity: 10, CostBasis: 100000}}
	action := CorporateAction{ID: "split", InstrumentID: "x", Kind: "split", Currency: "USD", RatioNumerator: 2, RatioDenominator: 1, EffectiveAt: at("2026-10-07T13:30:00Z")}
	m, e := ApplyCorporateAction(a, p, action, p)
	if e != nil || m.Positions[0].Quantity != 20 || m.Positions[0].CostBasis != 100000 || m.Entries[0].QuantityDelta != 10 {
		t.Fatal(m, e)
	}
}
func TestSplitFractionWithoutCashDataBlocks(t *testing.T) {
	a := Account{ID: "a"}
	p := []Position{{InstrumentID: "x", Quantity: 3, CostBasis: 12000}}
	action := CorporateAction{ID: "reverse", InstrumentID: "x", Kind: "split", Currency: "USD", RatioNumerator: 1, RatioDenominator: 2, EffectiveAt: at("2026-10-07T13:30:00Z")}
	if _, e := ApplyCorporateAction(a, p, action, p); !errors.Is(e, ErrCorporateActionIncomplete) {
		t.Fatal(e)
	}
	rate := Money(8000)
	action.CashInLieu = &rate
	action.CashInLieuUnit = "USD_per_post_split_share"
	m, e := ApplyCorporateAction(a, p, action, p)
	if e != nil || m.Positions[0].Quantity != 1 || m.Positions[0].CostBasis != 8000 || m.Account.Balances.Available != 4000 {
		t.Fatal(m, e)
	}
}
func TestDividendEligibilityIsPreExDate(t *testing.T) {
	a := Account{ID: "a"}
	current := []Position{{InstrumentID: "x", Quantity: 15, CostBasis: 100000}}
	eligible := []Position{{InstrumentID: "x", Quantity: 10}}
	action := CorporateAction{ID: "dividend", InstrumentID: "x", Kind: "dividend", Currency: "USD", Amount: 1000000, EffectiveAt: at("2026-10-07T13:30:00Z"), PayAt: at("2026-10-12T13:30:00Z")}
	m, e := ApplyCorporateAction(a, current, action, eligible)
	if e != nil || m.Account.Balances.Dividends != 1000 || m.Positions[0].Quantity != 15 {
		t.Fatal(m, e)
	}
}
func TestDividendNAVAndPaymentDoNotDoubleIncome(t *testing.T) {
	input := riskFixture()
	account := Account{ID: "a"}
	positions := []Position{{InstrumentID: "a", Quantity: 10, CostBasis: 100000}}
	before, e := ComputeNAV(account, positions, input.Snapshot, nil)
	if e != nil {
		t.Fatal(e)
	}
	action := CorporateAction{ID: "nav-dividend", InstrumentID: "a", Kind: "dividend", Currency: "USD", Amount: 1000000, EffectiveAt: at("2026-10-06T13:30:00Z"), PayAt: at("2026-10-07T13:30:00Z")}
	accrued, e := ApplyCorporateAction(account, positions, action, positions)
	if e != nil {
		t.Fatal(e)
	}
	exPrices := map[string]Price{"a": 99000000}
	during, e := ComputeNAV(accrued.Account, positions, input.Snapshot, exPrices)
	if e != nil || during != before {
		t.Fatal(before, during, e)
	}
	paid, e := PayDividend(accrued.Account, action, accrued.Entries[0], action.PayAt)
	if e != nil {
		t.Fatal(e)
	}
	after, e := ComputeNAV(paid.Account, positions, input.Snapshot, exPrices)
	if e != nil || after != before {
		t.Fatal(before, after, e)
	}
}
