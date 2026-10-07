package domain

import (
	"math"
	"testing"
	"time"
)

func quarterFacts() []FinancialFact {
	var out []FinancialFact
	for _, concept := range []string{"NetIncome", "Revenue", "OperatingCashFlow", "CapitalExpenditure", "DilutedEPS"} {
		for i, end := range []string{"2025-03-31", "2025-06-30", "2025-09-30", "2025-12-31"} {
			v := []string{"10", "30", "60", "100"}[i]
			if concept == "OperatingCashFlow" {
				v = []string{"12", "36", "72", "120"}[i]
			}
			if concept == "CapitalExpenditure" {
				v = []string{"5", "10", "15", "20"}[i]
			}
			unit := "USD"
			if concept == "DilutedEPS" {
				unit = "USD/shares"
			}
			out = append(out, FinancialFact{Concept: concept, Value: v, Unit: unit, Currency: "USD", Accession: end, PeriodStart: at("2025-01-01T00:00:00Z"), PeriodEnd: at(end + "T00:00:00Z"), AvailableAt: at("2026-02-01T00:00:00Z")})
		}
	}
	for _, end := range []string{"2024-12-31", "2025-12-31"} {
		out = append(out, FinancialFact{Concept: "Equity", Value: "200", Unit: "USD", Currency: "USD", PeriodEnd: at(end + "T00:00:00Z"), AvailableAt: at("2026-02-01T00:00:00Z")})
	}
	return out
}
func TestTTMUsesNonOverlappingQuarters(t *testing.T) {
	ttm, e := NormalizeTTM(quarterFacts(), at("2026-03-01T00:00:00Z"))
	if e != nil || ttm.NetIncome.Amount != "100" || ttm.FreeCashFlow.Amount != "100" {
		t.Fatal(ttm, e)
	}
}
func TestNonPositiveEPSHasNoPE(t *testing.T) {
	ttm, _ := NormalizeTTM(quarterFacts(), at("2026-03-01T00:00:00Z"))
	ttm.DilutedEPS.Amount = "-2"
	v, e := ComputeValuation(Price(10000000), ttm, FinancialValue{Amount: "1000000000", Unit: "shares"})
	if e != nil || v.PE.Value != nil || v.PE.Reason == "" || *v.MarketCap.Value != 1e10 {
		t.Fatal(v, e)
	}
}
func TestFinancialUnitAndCurrencyMismatch(t *testing.T) {
	f := quarterFacts()
	f[1].Currency = "EUR"
	if _, e := NormalizeTTM(f, at("2026-03-01T00:00:00Z")); e == nil {
		t.Fatal("mixed currencies")
	}
}
func TestTTMFiscalYearOpeningEquityAndEPSUnit(t *testing.T) {
	facts := quarterFacts()
	// 52-week fiscal year ends December 27, so opening equity is December 28 of the prior year.
	for n := range facts {
		f := &facts[n]
		if !f.PeriodStart.IsZero() {
			f.PeriodStart = f.PeriodStart.AddDate(0, 0, -3)
			f.PeriodEnd = f.PeriodEnd.AddDate(0, 0, -4)
		} else {
			if f.PeriodEnd.Year() == 2024 {
				f.PeriodEnd = at("2024-12-28T00:00:00Z")
				f.Value = "150"
			} else {
				f.PeriodEnd = at("2025-12-27T00:00:00Z")
			}
		}
	}
	// Explicit independent quarter starts avoid Gregorian quarter assumptions.
	ends := []string{"2025-03-27", "2025-06-26", "2025-09-25", "2025-12-27"}
	for n := range facts {
		if !facts[n].PeriodStart.IsZero() {
			facts[n].PeriodStart = at("2024-12-29T00:00:00Z")
			facts[n].PeriodEnd = at(ends[n%4] + "T00:00:00Z")
		}
	}
	v, e := NormalizeTTM(facts, at("2026-03-01T00:00:00Z"))
	if e != nil || v.OpeningEquity.Amount != "150" {
		t.Fatal(v.OpeningEquity, e)
	}
	v.DilutedEPS.Unit = "USD"
	valuation, e := ComputeValuation(10000000, v, FinancialValue{Amount: "1000", Unit: "shares"})
	if e != nil || valuation.PE.Value != nil || valuation.PE.Reason == "" {
		t.Fatal(valuation, e)
	}
}
func TestIndicatorsNoPadding(t *testing.T) {
	v, _ := ComputeIndicators([]Bar{{Close: 1000000}})
	if v.SMA200.Value != nil || v.SMA200.Reason == "" {
		t.Fatal(v)
	}
	v, _ = ComputeIndicators([]Bar{{Close: 100000000}, {Close: 120000000}, {Close: 90000000}})
	if math.Abs(*v.MaxDrawdown.Value-.25) > 1e-10 {
		t.Fatal(v.MaxDrawdown)
	}
}
func TestRSIBoundaries(t *testing.T) {
	for _, c := range []struct {
		step int64
		want float64
	}{{0, 50}, {1000000, 100}, {-1000000, 0}} {
		var bars []Bar
		for i := 0; i < 20; i++ {
			bars = append(bars, Bar{Close: Price(100000000 + int64(i)*c.step), SessionDate: time.Unix(int64(i)*86400, 0)})
		}
		v, e := ComputeIndicators(bars)
		if e != nil || v.RSI14.Value == nil || *v.RSI14.Value != c.want {
			t.Fatal(v.RSI14, e)
		}
	}
}
