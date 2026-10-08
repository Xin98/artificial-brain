package domain

import "time"

type RiskAssessment struct {
	Level   string
	Reasons []string
	AsOf    time.Time
}
type Recommendation struct {
	Action, Potential, StrategyVersionID, UniverseVersionID string
	AsOf                                                    time.Time
	Evidence, Unknowns                                      []string
}

func ClassifyRisk(i Indicators) RiskAssessment {
	r := RiskAssessment{Level: "unknown", Reasons: []string{}}
	if i.AnnualVolatility.Value == nil || i.MaxDrawdown.Value == nil {
		r.Reasons = append(r.Reasons, "price_risk_metrics_incomplete")
		return r
	}
	v, d := *i.AnnualVolatility.Value, *i.MaxDrawdown.Value
	r.Level = "low"
	if v > .6 || d > .35 {
		r.Level = "high"
	} else if v > .3 || d > .2 {
		r.Level = "medium"
	}
	if v > .3 {
		r.Reasons = append(r.Reasons, "annual_volatility_elevated")
	}
	if d > .2 {
		r.Reasons = append(r.Reasons, "historical_drawdown_elevated")
	}
	return r
}
func Explain(signal Signal, metrics FinancialMetrics, risk RiskAssessment) (Recommendation, error) {
	r := Recommendation{Action: "observe", Potential: "weak", Evidence: []string{"relative_factor_score_not_profit_probability", "unvalidated_research_weights"}, Unknowns: []string{}}
	if signal.Score >= 70 {
		r.Potential = "high"
		r.Action = "consider_after_account_risk_checks"
	} else if signal.Score >= 50 {
		r.Potential = "observe"
	}
	if risk.Level == "high" {
		r.Action = "avoid_new_automatic_buy"
		r.Evidence = append(r.Evidence, "high_price_risk")
	} else if risk.Level == "unknown" {
		r.Action = "insufficient_evidence"
		r.Unknowns = append(r.Unknowns, risk.Reasons...)
	}
	if metrics.Valuation.PE.Value == nil {
		r.Unknowns = append(r.Unknowns, "pe_unavailable")
	}
	return r, nil
}
