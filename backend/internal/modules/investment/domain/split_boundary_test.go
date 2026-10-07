package domain

import "testing"

func TestSplitDoesNotReadjustEffectiveSession(t *testing.T) {
	bars := []Bar{{InstrumentID: "a", SessionDate: at("2026-10-05T00:00:00Z"), Open: 100000000, High: 100000000, Low: 100000000, Close: 100000000}, {InstrumentID: "a", SessionDate: at("2026-10-06T00:00:00Z"), Open: 50000000, High: 50000000, Low: 50000000, Close: 50000000}}
	action := CorporateAction{InstrumentID: "a", Kind: "split", EffectiveAt: at("2026-10-06T13:30:00Z"), AvailableAt: at("2026-10-05T20:00:00Z"), RatioNumerator: 2, RatioDenominator: 1}
	got, e := AdjustedBars(bars, []CorporateAction{action}, at("2026-10-06T21:00:00Z"))
	if e != nil || got[0].Close != 50000000 || got[1].Close != 50000000 || bars[0].Close != 100000000 {
		t.Fatal(got, e)
	}
}

func TestValuationRejectsSharesStillReportedBeforeOlderSplit(t *testing.T) {
	facts := append(quarterFacts(), FinancialFact{Concept: "Shares", Value: "100", Unit: "shares", PeriodEnd: at("2024-03-31T00:00:00Z"), AvailableAt: at("2024-05-01T00:00:00Z")})
	action := CorporateAction{Kind: "split", EffectiveAt: at("2024-06-03T13:30:00Z"), AvailableAt: at("2024-05-01T00:00:00Z"), RatioNumerator: 2, RatioDenominator: 1}
	got, e := ComputeFinancialMetrics(facts, []Bar{{Close: 50000000}}, at("2026-03-01T00:00:00Z"), action)
	if e != nil || got.Valuation.MarketCap.Value != nil || got.Valuation.MarketCap.Reason != "split_reporting_basis_unverified" {
		t.Fatal(got.Valuation, e)
	}
}
