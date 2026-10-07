package domain

import (
	"math"
	"testing"
)

func TestPercentilesAverageTies(t *testing.T) {
	scores, e := RankPercentiles([]float64{1, 2, 2, 4}, true)
	if e != nil || scores[0] != 0 || scores[1] != 50 || scores[2] != 50 || scores[3] != 100 {
		t.Fatal(scores, e)
	}
	scores, _ = RankPercentiles([]float64{1, 2, 2, 4}, false)
	if scores[0] != 100 || scores[3] != 0 {
		t.Fatal(scores)
	}
	if _, e = RankPercentiles([]float64{math.NaN(), 1}, true); e == nil {
		t.Fatal("nonfinite")
	}
}
func TestFixedFactorWeights(t *testing.T) {
	p := DefaultStrategyParameters()
	if WeightedScore([5]float64{100, 50, 0, 50, 100}, p) != 67.5 {
		t.Fatal(p)
	}
}
func TestInsufficientUniverseDoesNotReweight(t *testing.T) {
	_, e := Evaluate(Snapshot{}, UniverseVersion{InstrumentIDs: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}}, StrategyVersion{Parameters: DefaultStrategyParameters()})
	if e != ErrInsufficientUniverse {
		t.Fatal(e)
	}
}
func TestHighPotentialCanHaveHighRisk(t *testing.T) {
	risk := ClassifyRisk(Indicators{AnnualVolatility: valueMetric(.65), MaxDrawdown: valueMetric(.15)})
	r, e := Explain(Signal{Score: 80}, FinancialMetrics{}, risk)
	if e != nil || risk.Level != "high" || r.Potential != "high" {
		t.Fatal(r, risk, e)
	}
	risk = ClassifyRisk(Indicators{AnnualVolatility: valueMetric(.6), MaxDrawdown: valueMetric(.2)})
	if risk.Level != "medium" {
		t.Fatal(risk)
	}
}

func TestMissingRiskInputsAndIndustryMap(t *testing.T) {
	for _, i := range []Indicators{{AnnualVolatility: valueMetric(.2)}, {MaxDrawdown: valueMetric(.1)}} {
		if ClassifyRisk(i).Level != "unknown" {
			t.Fatal(i)
		}
	}
	for _, c := range []struct{ sic, want string }{{"3571", "manufacturing"}, {"6021", "finance"}, {"4813", "transport_utilities"}, {"5311", "retail"}, {"9999", "unknown"}, {"", "unknown"}} {
		if Industry(c.sic) != c.want {
			t.Fatal(c)
		}
	}
}
