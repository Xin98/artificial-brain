package domain

import (
	"math"
	"sort"
	"time"
)

type FactorEvidence struct {
	Group                  string
	RawValues, Percentiles []float64
	Weight, Contribution   float64
	FactRefs               []string
}
type Signal struct {
	InstrumentID, Industry string
	Score                  float64
	Rank                   int
	FactorEvidence         []FactorEvidence
	QualityFlags           []string
	Metrics                FinancialMetrics
	Risk                   RiskAssessment
	Recommendation         Recommendation
}
type Exclusion struct{ InstrumentID, Reason string }
type Evaluation struct {
	SessionDate                                                                    time.Time
	Purpose                                                                        string
	FrozenPolicy                                                                   RiskPolicy
	OrderIDs                                                                       []string
	JobID                                                                          int64
	ID, AccountID, SnapshotID, StrategyVersionID, UniverseVersionID, State, Reason string
	AsOf                                                                           time.Time
	Signals                                                                        []Signal
	Excluded                                                                       []Exclusion
	QualityFlags                                                                   []string
	DatasetVersion, Mode                                                           string
	IssuedOrders                                                                   bool
}

func RankPercentiles(values []float64, higherIsBetter bool) ([]float64, error) {
	if len(values) < 2 {
		return nil, ErrInsufficientUniverse
	}
	type row struct {
		v float64
		i int
	}
	rows := make([]row, len(values))
	for i, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, ErrFactorUnavailable
		}
		rows[i] = row{v, i}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].v < rows[j].v })
	out := make([]float64, len(values))
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].v == rows[start].v {
			end++
		}
		score := float64(start+end-1) / 2 / float64(len(rows)-1) * 100
		if !higherIsBetter {
			score = 100 - score
		}
		for _, r := range rows[start:end] {
			out[r.i] = score
		}
		start = end
	}
	return out, nil
}
func WeightedScore(groups [5]float64, p StrategyParameters) float64 {
	v := 0.0
	for i, g := range groups {
		v += g * p.Weights[i]
	}
	return math.Round(v*10) / 10
}
func Evaluate(snapshot Snapshot, universe UniverseVersion, strategy StrategyVersion) (Evaluation, error) {
	out := Evaluation{SnapshotID: snapshot.ID, StrategyVersionID: strategy.ID, UniverseVersionID: universe.ID, AsOf: snapshot.AsOf, State: "completed", Signals: []Signal{}, Excluded: []Exclusion{}, QualityFlags: append([]string(nil), snapshot.QualityFlags...)}
	if e := strategy.Parameters.Validate(); e != nil {
		return out, e
	}
	s, e := SelectSnapshot(snapshot, snapshot.AsOf)
	if e != nil {
		return out, e
	}
	instruments := map[string]Instrument{}
	for _, i := range s.Instruments {
		instruments[i.ID] = i
	}
	type candidate struct {
		signal Signal
		raw    [9]float64
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, id := range universe.InstrumentIDs {
		if seen[id] {
			return out, ErrInvalidInput
		}
		seen[id] = true
		i, ok := instruments[id]
		reason := ""
		industry := Industry(i.SIC)
		if !ok || i.Kind != "stock" || !i.Tradable {
			reason = "instrument_ineligible"
		} else if industry == "unknown" || industry == "finance" {
			reason = "strategy_industry_excluded"
		}
		var bars []Bar
		var facts []FinancialFact
		var actions []CorporateAction
		for _, b := range s.Bars {
			if b.InstrumentID == id {
				bars = append(bars, b)
			}
		}
		for _, f := range s.Facts {
			if f.InstrumentID == id {
				facts = append(facts, f)
			}
		}
		for _, a := range s.Actions {
			if a.InstrumentID == id {
				actions = append(actions, a)
				if a.Kind != "split" && a.Kind != "dividend" {
					reason = "corporate_action_incomplete"
				}
			}
		}
		if len(bars) < 201 {
			reason = "insufficient_price_history"
		}
		if reason != "" {
			out.Excluded = append(out.Excluded, Exclusion{id, reason})
			continue
		}
		bars, e = AdjustedBars(bars, actions, s.AsOf)
		if e != nil {
			out.Excluded = append(out.Excluded, Exclusion{id, e.Error()})
			continue
		}
		latest, ce := s.Calendar.LatestCompleted(s.AsOf)
		if ce != nil || !bars[len(bars)-1].SessionDate.Equal(latest.Date) {
			out.Excluded = append(out.Excluded, Exclusion{id, "data_stale"})
			continue
		}
		metrics, me := ComputeFinancialMetrics(facts, bars, s.AsOf)
		if me != nil || s.AsOf.Sub(metrics.TTM.LatestPeriodEnd) > 180*24*time.Hour {
			reason = "factor_unavailable"
			if me == nil {
				reason = "financial_report_stale"
			}
			out.Excluded = append(out.Excluded, Exclusion{id, reason})
			continue
		}
		ocfMargin := Ratio(metrics.TTM.OperatingCashFlow, metrics.TTM.Revenue, true)
		required := []Metric{metrics.Indicators.SMA50, metrics.Indicators.SMA200, metrics.Indicators.AnnualVolatility, metrics.Valuation.EarningsYield, metrics.Valuation.FreeCashFlowYield, metrics.ROE, ocfMargin}
		complete := true
		for _, m := range required {
			if m.Value == nil {
				complete = false
			}
		}
		if !complete {
			out.Excluded = append(out.Excluded, Exclusion{id, "factor_unavailable"})
			continue
		}
		equity, _ := rat(averageEquity(metrics.TTM))
		if equity == nil || equity.Sign() <= 0 {
			out.Excluded = append(out.Excluded, Exclusion{id, "nonpositive_equity"})
			continue
		}
		n := len(bars)
		close := float64(bars[n-1].Close) / 1e6
		raw := [9]float64{float64(bars[n-1].Close)/float64(bars[n-64].Close) - 1, float64(bars[n-1].Close)/float64(bars[n-127].Close) - 1, close / *metrics.Indicators.SMA50.Value - 1, *metrics.Indicators.SMA50.Value / *metrics.Indicators.SMA200.Value - 1, *metrics.Indicators.AnnualVolatility.Value, *metrics.Valuation.EarningsYield.Value, *metrics.Valuation.FreeCashFlowYield.Value, *metrics.ROE.Value, *ocfMargin.Value}
		risk := ClassifyRisk(metrics.Indicators)
		risk.AsOf = s.AsOf
		candidates = append(candidates, candidate{signal: Signal{InstrumentID: id, Industry: industry, Metrics: metrics, Risk: risk, QualityFlags: []string{"relative_score_not_probability"}}, raw: raw})
	}
	if len(candidates) < 10 {
		out.State = "insufficient_universe"
		out.Reason = ErrInsufficientUniverse.Error()
		return out, ErrInsufficientUniverse
	}
	var ranks [9][]float64
	for f := 0; f < 9; f++ {
		values := make([]float64, len(candidates))
		for i, c := range candidates {
			values[i] = c.raw[f]
		}
		ranks[f], e = RankPercentiles(values, f != 4)
		if e != nil {
			return out, e
		}
	}
	groupIndexes := [][]int{{0, 1}, {2, 3}, {4}, {5, 6}, {7, 8}}
	names := []string{"momentum", "trend", "low_volatility", "value", "quality"}
	for i, c := range candidates {
		var groups [5]float64
		for g, indexes := range groupIndexes {
			priceRefs := []string{"snapshot:" + snapshot.ID + "/bars/" + c.signal.InstrumentID}
			refs := priceRefs
			switch g {
			case 3:
				refs = append(append(append([]string(nil), priceRefs...), c.signal.Metrics.Valuation.EarningsYield.FactRefs...), c.signal.Metrics.Valuation.FreeCashFlowYield.FactRefs...)
			case 4:
				refs = append(append(append([]string(nil), c.signal.Metrics.ROE.FactRefs...), c.signal.Metrics.TTM.OperatingCashFlow.FactRefs...), c.signal.Metrics.TTM.Revenue.FactRefs...)
			}
			ev := FactorEvidence{Group: names[g], Weight: strategy.Parameters.Weights[g], FactRefs: refs}
			for _, f := range indexes {
				ev.RawValues = append(ev.RawValues, c.raw[f])
				ev.Percentiles = append(ev.Percentiles, ranks[f][i])
				groups[g] += ranks[f][i] / float64(len(indexes))
			}
			ev.Contribution = groups[g] * ev.Weight
			c.signal.FactorEvidence = append(c.signal.FactorEvidence, ev)
		}
		c.signal.Score = WeightedScore(groups, strategy.Parameters)
		c.signal.Recommendation, _ = Explain(c.signal, c.signal.Metrics, c.signal.Risk)
		c.signal.Recommendation.AsOf = s.AsOf
		c.signal.Recommendation.StrategyVersionID = strategy.ID
		c.signal.Recommendation.UniverseVersionID = universe.ID
		out.Signals = append(out.Signals, c.signal)
	}
	sort.Slice(out.Signals, func(i, j int) bool {
		a, b := out.Signals[i], out.Signals[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.InstrumentID < b.InstrumentID
	})
	for i := range out.Signals {
		out.Signals[i].Rank = i + 1
	}
	return out, nil
}
