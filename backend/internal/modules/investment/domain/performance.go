package domain

import (
	"math"
)

type Performance struct {
	CumulativeReturn float64  `json:"cumulativeReturn"`
	MaxDrawdown      float64  `json:"maxDrawdown"`
	Turnover         float64  `json:"turnover"`
	AnnualReturn     *float64 `json:"annualReturn"`
	Sharpe           *float64 `json:"sharpe"`
	MissingReasons   []string `json:"missingReasons"`
	TradingDays      int      `json:"tradingDays"`
}

func ComputePerformance(curve []NAVPoint, fills []Fill, initial Money) (Performance, error) {
	out := Performance{MissingReasons: []string{}, TradingDays: len(curve)}
	if initial <= 0 || len(curve) == 0 {
		return out, ErrDataStale
	}
	peak, previous := initial, initial
	total := 0.0
	returns := []float64{}
	for _, p := range curve {
		if p.NAV < 0 || previous <= 0 {
			return out, ErrInvalidInput
		}
		if p.NAV > peak {
			peak = p.NAV
		}
		dd := 1 - float64(p.NAV)/float64(peak)
		if dd > out.MaxDrawdown {
			out.MaxDrawdown = dd
		}
		returns = append(returns, float64(p.NAV)/float64(previous)-1)
		previous = p.NAV
		total += float64(p.NAV)
	}
	out.CumulativeReturn = float64(previous)/float64(initial) - 1
	gross := 0.0
	for _, f := range fills {
		if f.Gross < 0 {
			return out, ErrInvalidInput
		}
		gross += float64(f.Gross)
	}
	if total > 0 {
		out.Turnover = gross / (total / float64(len(curve)))
	}
	if len(curve) >= 252 {
		v := math.Pow(float64(previous)/float64(initial), 252/float64(len(curve))) - 1
		out.AnnualReturn = &v
	} else {
		out.MissingReasons = append(out.MissingReasons, "annual_requires_252_trading_days")
	}
	if len(returns) < 60 {
		out.MissingReasons = append(out.MissingReasons, "sharpe_requires_60_daily_returns")
	} else {
		mean := 0.0
		for _, v := range returns {
			mean += v
		}
		mean /= float64(len(returns))
		variance := 0.0
		for _, v := range returns {
			variance += (v - mean) * (v - mean)
		}
		variance /= float64(len(returns) - 1)
		if variance < 1e-24 {
			out.MissingReasons = append(out.MissingReasons, "sharpe_zero_variance")
		} else {
			v := mean / math.Sqrt(variance) * math.Sqrt(252)
			out.Sharpe = &v
		}
	}
	return out, nil
}
