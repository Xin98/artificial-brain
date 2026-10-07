package domain

import (
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

type FinancialValue struct {
	Amount, Unit, Currency, Reason string
	FactRefs                       []string
}
type Metric struct {
	Value    *float64
	Reason   string
	FactRefs []string
}
type TTMFinancials struct {
	NetIncome, Revenue, OperatingCashFlow, CapitalExpenditure, FreeCashFlow, DilutedEPS, OpeningEquity, ClosingEquity FinancialValue
	LatestPeriodEnd                                                                                                   time.Time
}
type Valuation struct{ PE, PB, MarketCap, EarningsYield, FreeCashFlowYield Metric }
type FinancialMetrics struct {
	TTM                                         TTMFinancials
	Valuation                                   Valuation
	Indicators                                  Indicators
	ProfitGrowth, RevenueGrowth, DebtRatio, ROE Metric
}

var financialDecimal = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

func rat(v FinancialValue) (*big.Rat, error) {
	if v.Reason != "" || !financialDecimal.MatchString(v.Amount) {
		return nil, ErrFactorUnavailable
	}
	r, ok := new(big.Rat).SetString(v.Amount)
	if !ok {
		return nil, ErrFactorUnavailable
	}
	return r, nil
}
func unavailable(reason string) Metric { return Metric{Reason: reason} }
func metric(v *big.Rat, refs []string) Metric {
	f, _ := v.Float64()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return unavailable("numeric_overflow")
	}
	return Metric{Value: &f, FactRefs: refs}
}
func decimalRat(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	s := r.FloatString(12)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}
func Ratio(a, b FinancialValue, positiveDenominator bool) Metric {
	if a.Currency != b.Currency && b.Currency != "" {
		return unavailable("currency_mismatch")
	}
	x, e := rat(a)
	if e != nil {
		return unavailable("factor_unavailable")
	}
	y, e := rat(b)
	if e != nil || y.Sign() == 0 || (positiveDenominator && y.Sign() < 0) {
		return unavailable("nonpositive_denominator")
	}
	return metric(new(big.Rat).Quo(x, y), append(append([]string(nil), a.FactRefs...), b.FactRefs...))
}
func selectedFacts(facts []FinancialFact, asOf time.Time) []FinancialFact {
	s, _ := SelectSnapshot(Snapshot{Facts: facts}, asOf)
	return s.Facts
}
func instant(facts []FinancialFact, concept string, by time.Time) FinancialValue {
	var chosen *FinancialFact
	for i := range facts {
		f := &facts[i]
		if f.Concept == concept && !f.PeriodEnd.After(by) && (chosen == nil || f.PeriodEnd.After(chosen.PeriodEnd)) {
			chosen = f
		}
	}
	if chosen == nil {
		return FinancialValue{Reason: "fact_unavailable"}
	}
	return FinancialValue{Amount: chosen.Value, Unit: chosen.Unit, Currency: chosen.Currency, FactRefs: []string{chosen.Accession + "/" + chosen.Concept}}
}
func ttmValue(facts []FinancialFact, concept string) (FinancialValue, time.Time, error) {
	var list []FinancialFact
	for _, f := range facts {
		if f.Concept == concept {
			list = append(list, f)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].PeriodEnd.Equal(list[j].PeriodEnd) {
			return list[i].PeriodEnd.Before(list[j].PeriodEnd)
		}
		return list[i].PeriodStart.After(list[j].PeriodStart)
	})
	type quarter struct {
		end, start     time.Time
		value          *big.Rat
		refs           []string
		unit, currency string
	}
	var quarters []quarter
	for _, f := range list {
		if f.PeriodStart.IsZero() {
			continue
		}
		days := f.PeriodEnd.Sub(f.PeriodStart).Hours()/24 + 1
		if days < 70 || days > 380 {
			continue
		}
		value, e := rat(FinancialValue{Amount: f.Value})
		if e != nil {
			return FinancialValue{}, time.Time{}, e
		}
		refs := []string{f.Accession + "/" + f.Concept}
		start := f.PeriodStart
		if days > 110 {
			var predecessor *FinancialFact
			for j := range list {
				p := &list[j]
				if p.PeriodStart.Equal(f.PeriodStart) && p.PeriodEnd.Before(f.PeriodEnd) && (predecessor == nil || p.PeriodEnd.After(predecessor.PeriodEnd)) {
					predecessor = p
				}
			}
			if predecessor == nil {
				continue
			}
			gap := f.PeriodEnd.Sub(predecessor.PeriodEnd).Hours() / 24
			if gap < 70 || gap > 110 {
				continue
			}
			if predecessor.Unit != f.Unit || predecessor.Currency != f.Currency {
				return FinancialValue{}, time.Time{}, ErrFactorUnavailable
			}
			prior, e := rat(FinancialValue{Amount: predecessor.Value})
			if e != nil {
				return FinancialValue{}, time.Time{}, e
			}
			value.Sub(value, prior)
			refs = append(refs, predecessor.Accession+"/"+concept)
			start = predecessor.PeriodEnd.AddDate(0, 0, 1)
		}
		q := quarter{f.PeriodEnd, start, value, refs, f.Unit, f.Currency}
		if len(quarters) > 0 && quarters[len(quarters)-1].end.Equal(f.PeriodEnd) {
			continue
		}
		quarters = append(quarters, q)
	}
	if len(quarters) < 4 {
		return FinancialValue{Reason: "four_quarters_unavailable"}, time.Time{}, ErrFactorUnavailable
	}
	quarters = quarters[len(quarters)-4:]
	sum := new(big.Rat)
	var refs []string
	unit, currency := quarters[0].unit, quarters[0].currency
	for i, q := range quarters {
		if q.unit != unit || q.currency != currency {
			return FinancialValue{}, time.Time{}, ErrFactorUnavailable
		}
		if i > 0 && !q.start.Equal(quarters[i-1].end.AddDate(0, 0, 1)) {
			return FinancialValue{}, time.Time{}, ErrFactorUnavailable
		}
		sum.Add(sum, q.value)
		refs = append(refs, q.refs...)
	}
	return FinancialValue{Amount: decimalRat(sum), Unit: unit, Currency: currency, FactRefs: refs}, quarters[3].end, nil
}
func NormalizeTTM(facts []FinancialFact, asOf time.Time) (TTMFinancials, error) {
	facts = selectedFacts(facts, asOf)
	t := TTMFinancials{}
	fields := []struct {
		concept string
		v       *FinancialValue
	}{{"NetIncome", &t.NetIncome}, {"Revenue", &t.Revenue}, {"OperatingCashFlow", &t.OperatingCashFlow}, {"CapitalExpenditure", &t.CapitalExpenditure}}
	for _, f := range fields {
		v, end, e := ttmValue(facts, f.concept)
		if e != nil {
			return t, e
		}
		if v.Unit != "USD" || v.Currency != "USD" {
			return t, ErrFactorUnavailable
		}
		if !t.LatestPeriodEnd.IsZero() && !end.Equal(t.LatestPeriodEnd) {
			return t, ErrFactorUnavailable
		}
		t.LatestPeriodEnd = end
		*f.v = v
	}
	t.DilutedEPS, _, _ = ttmValue(facts, "DilutedEPS")
	if t.DilutedEPS.Amount == "" {
		t.DilutedEPS.Reason = "eps_unavailable"
	}
	cap, _ := rat(t.CapitalExpenditure)
	cap.Abs(cap)
	t.CapitalExpenditure.Amount = decimalRat(cap)
	ocf, _ := rat(t.OperatingCashFlow)
	t.FreeCashFlow = FinancialValue{Amount: decimalRat(new(big.Rat).Sub(ocf, cap)), Unit: "USD", Currency: "USD", FactRefs: append(append([]string(nil), t.OperatingCashFlow.FactRefs...), t.CapitalExpenditure.FactRefs...)}
	t.ClosingEquity = instant(facts, "Equity", t.LatestPeriodEnd)
	t.OpeningEquity = instant(facts, "Equity", t.LatestPeriodEnd.AddDate(-1, 0, 0))
	return t, nil
}
func ComputeValuation(price Price, ttm TTMFinancials, shares FinancialValue) (Valuation, error) {
	if price <= 0 {
		return Valuation{}, ErrInvalidInput
	}
	p := new(big.Rat).SetFrac64(int64(price), 1000000)
	share, e := rat(shares)
	if e != nil || share.Sign() <= 0 || shares.Unit != "shares" {
		return Valuation{}, ErrFactorUnavailable
	}
	cap := new(big.Rat).Mul(p, share)
	capital := FinancialValue{Amount: decimalRat(cap), Currency: "USD", Unit: "USD", FactRefs: shares.FactRefs}
	priceValue := FinancialValue{Amount: decimalRat(p), Currency: "USD", Unit: "USD/shares"}
	return Valuation{MarketCap: metric(cap, shares.FactRefs), PE: Ratio(priceValue, ttm.DilutedEPS, true), PB: Ratio(capital, ttm.ClosingEquity, true), EarningsYield: Ratio(ttm.NetIncome, capital, true), FreeCashFlowYield: Ratio(ttm.FreeCashFlow, capital, true)}, nil
}
func averageEquity(t TTMFinancials) FinancialValue {
	a, e := rat(t.OpeningEquity)
	b, be := rat(t.ClosingEquity)
	if e != nil || be != nil || t.OpeningEquity.Currency != t.ClosingEquity.Currency {
		return FinancialValue{Reason: "equity_unavailable"}
	}
	return FinancialValue{Amount: decimalRat(new(big.Rat).Quo(new(big.Rat).Add(a, b), big.NewRat(2, 1))), Currency: t.ClosingEquity.Currency, Unit: t.ClosingEquity.Unit, FactRefs: append(append([]string(nil), t.OpeningEquity.FactRefs...), t.ClosingEquity.FactRefs...)}
}
func ComputeFinancialMetrics(facts []FinancialFact, bars []Bar, asOf time.Time) (FinancialMetrics, error) {
	ind, e := ComputeIndicators(bars)
	m := FinancialMetrics{Indicators: ind}
	if e != nil {
		return m, e
	}
	t, e := NormalizeTTM(facts, asOf)
	m.TTM = t
	if e != nil {
		return m, e
	}
	selected := selectedFacts(facts, asOf)
	m.Valuation, e = ComputeValuation(bars[len(bars)-1].Close, t, instant(selected, "Shares", t.LatestPeriodEnd))
	m.ROE = Ratio(t.NetIncome, averageEquity(t), true)
	m.DebtRatio = Ratio(instant(selected, "Debt", t.LatestPeriodEnd), instant(selected, "Assets", t.LatestPeriodEnd), true)
	priorFacts := make([]FinancialFact, 0)
	for _, f := range selected {
		if !f.PeriodEnd.After(t.LatestPeriodEnd.AddDate(-1, 0, 0)) {
			priorFacts = append(priorFacts, f)
		}
	}
	prior, pe := NormalizeTTM(priorFacts, asOf)
	if pe != nil {
		m.ProfitGrowth = unavailable("comparable_period_unavailable")
		m.RevenueGrowth = unavailable("comparable_period_unavailable")
	} else {
		m.ProfitGrowth = growth(t.NetIncome, prior.NetIncome)
		m.RevenueGrowth = growth(t.Revenue, prior.Revenue)
	}
	return m, e
}
func growth(current, previous FinancialValue) Metric {
	r := Ratio(current, previous, true)
	if r.Value != nil {
		v := *r.Value - 1
		r.Value = &v
	}
	return r
}
