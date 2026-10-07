package domain

import (
	"math"
	"strconv"
	"time"
)

const IndustryMapVersion = "sic-major-groups-v1"

type StrategyParameters struct {
	Weights                              [5]float64
	EntryScore, ExitScore, RebalanceBand float64
	MaxHoldings                          int
}

func DefaultStrategyParameters() StrategyParameters {
	return StrategyParameters{Weights: [5]float64{.3, .2, .15, .15, .2}, EntryScore: 70, ExitScore: 50, RebalanceBand: .02, MaxHoldings: 10}
}
func (p StrategyParameters) Validate() error {
	sum := 0.0
	for _, w := range p.Weights {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 || w > 1 {
			return ErrInvalidInput
		}
		sum += w
	}
	if math.Abs(sum-1) > 1e-9 || math.IsNaN(p.EntryScore) || math.IsNaN(p.ExitScore) || math.IsNaN(p.RebalanceBand) || p.EntryScore > 100 || p.EntryScore <= p.ExitScore || p.ExitScore < 0 || p.RebalanceBand < 0 || p.RebalanceBand > 1 || p.MaxHoldings < 1 || p.MaxHoldings > 10 {
		return ErrInvalidInput
	}
	return nil
}

type StrategyVersion struct {
	ID, StrategyID string
	Scope          Scope
	Parameters     StrategyParameters
	CreatedAt      time.Time
}
type UniverseVersion struct {
	ID, UniverseID, Name, Mode string
	Scope                      Scope
	InstrumentIDs              []string
	EffectiveAt, CreatedAt     time.Time
	HistoricalMembershipKnown  bool
}

func Industry(sic string) string {
	n, e := strconv.Atoi(sic)
	if e != nil || len(sic) != 4 {
		return "unknown"
	}
	for _, r := range []struct {
		lo, hi int
		name   string
	}{{100, 999, "agriculture"}, {1000, 1499, "mining"}, {1500, 1799, "construction"}, {2000, 3999, "manufacturing"}, {4000, 4999, "transport_utilities"}, {5000, 5199, "wholesale"}, {5200, 5999, "retail"}, {6000, 6799, "finance"}, {7000, 8999, "services"}} {
		if n >= r.lo && n <= r.hi {
			return r.name
		}
	}
	return "unknown"
}
