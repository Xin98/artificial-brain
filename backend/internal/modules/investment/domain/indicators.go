package domain

import (
	"math"
	"sort"
)

type Indicators struct {
	SMA20, SMA50, SMA200, RSI14, AnnualVolatility, MaxDrawdown, AverageVolume20, AverageTurnover20 Metric
	WindowDays                                                                                     int
}

func valueMetric(v float64) Metric {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return unavailable("nonfinite_value")
	}
	return Metric{Value: &v}
}
func sampleStd(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	return math.Sqrt(variance / float64(len(values)-1))
}
func ComputeIndicators(input []Bar) (Indicators, error) {
	missing := unavailable("insufficient_price_history")
	out := Indicators{SMA20: missing, SMA50: missing, SMA200: missing, RSI14: missing, AnnualVolatility: missing, MaxDrawdown: missing, AverageVolume20: missing, AverageTurnover20: missing, WindowDays: len(input)}
	if len(input) == 0 {
		return out, ErrFactorUnavailable
	}
	bars := append([]Bar(nil), input...)
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].SessionDate.Before(bars[j].SessionDate) })
	for _, b := range bars {
		if b.Close <= 0 || b.Volume < 0 {
			return out, ErrInvalidInput
		}
	}
	for _, x := range []struct {
		n int
		m *Metric
	}{{20, &out.SMA20}, {50, &out.SMA50}, {200, &out.SMA200}} {
		if len(bars) < x.n {
			continue
		}
		sum := 0.0
		for _, b := range bars[len(bars)-x.n:] {
			sum += float64(b.Close) / 1e6
		}
		*x.m = valueMetric(sum / float64(x.n))
	}
	if len(bars) >= 15 {
		gain, loss := 0.0, 0.0
		for i := 1; i < len(bars); i++ {
			change := float64(bars[i].Close - bars[i-1].Close)
			g, l := math.Max(change, 0), math.Max(-change, 0)
			if i <= 14 {
				gain += g / 14
				loss += l / 14
			} else {
				gain = (gain*13 + g) / 14
				loss = (loss*13 + l) / 14
			}
		}
		rsi := 50.0
		if loss == 0 && gain > 0 {
			rsi = 100
		} else if loss > 0 {
			rsi = 100 - 100/(1+gain/loss)
		}
		out.RSI14 = valueMetric(rsi)
	}
	if len(bars) >= 21 {
		var returns []float64
		for i := len(bars) - 20; i < len(bars); i++ {
			returns = append(returns, float64(bars[i].Close)/float64(bars[i-1].Close)-1)
		}
		out.AnnualVolatility = valueMetric(sampleStd(returns) * math.Sqrt(252))
	}
	start := 0
	if len(bars) > 252 {
		start = len(bars) - 252
	}
	peak, drawdown := 0.0, 0.0
	for _, b := range bars[start:] {
		v := float64(b.Close)
		peak = math.Max(peak, v)
		drawdown = math.Max(drawdown, 1-v/peak)
	}
	out.MaxDrawdown = valueMetric(drawdown)
	if len(bars) >= 20 {
		volume, turnover := 0.0, 0.0
		for _, b := range bars[len(bars)-20:] {
			volume += float64(b.Volume)
			turnover += float64(b.Volume) * float64(b.Close) / 1e6
		}
		out.AverageVolume20 = valueMetric(volume / 20)
		out.AverageTurnover20 = valueMetric(turnover / 20)
	}
	return out, nil
}
