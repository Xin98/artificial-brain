package domain

import (
	"math"
	"time"
)

type Scope struct{ WorkspaceID, OwnerUserID string }
type Balances struct{ Available, Reserved, Unsettled, Dividends Money }

func (b Balances) Apply(d Balances) (Balances, error) {
	out := Balances{}
	pairs := []struct {
		a, c   Money
		target *Money
	}{{b.Available, d.Available, &out.Available}, {b.Reserved, d.Reserved, &out.Reserved}, {b.Unsettled, d.Unsettled, &out.Unsettled}, {b.Dividends, d.Dividends, &out.Dividends}}
	for _, p := range pairs {
		v, e := addMoney(p.a, p.c)
		if e != nil {
			return Balances{}, e
		}
		if v < 0 {
			return Balances{}, ErrInsufficientCash
		}
		*p.target = v
	}
	return out, nil
}
func (b Balances) Total() (Money, error) {
	v := Money(0)
	for _, m := range []Money{b.Available, b.Reserved, b.Unsettled, b.Dividends} {
		var e error
		v, e = addMoney(v, m)
		if e != nil {
			return 0, e
		}
	}
	return v, nil
}

type RiskPolicy struct {
	SingleWeight   float64 `json:"singleWeight"`
	IndustryWeight float64 `json:"industryWeight"`
	StockWeight    float64 `json:"stockWeight"`
	DrawdownPause  float64 `json:"drawdownPause"`
	StopLoss       float64 `json:"stopLoss"`
	TurnoverLimit  float64 `json:"turnoverLimit"`
}

func DefaultRiskPolicy() RiskPolicy { return RiskPolicy{.1, .3, .8, .15, .1, .2} }

func (p RiskPolicy) Validate() error {
	d := DefaultRiskPolicy()
	for _, v := range []struct{ value, limit float64 }{{p.SingleWeight, d.SingleWeight}, {p.IndustryWeight, d.IndustryWeight}, {p.StockWeight, d.StockWeight}, {p.DrawdownPause, d.DrawdownPause}, {p.StopLoss, d.StopLoss}, {p.TurnoverLimit, d.TurnoverLimit}} {
		if math.IsNaN(v.value) || math.IsInf(v.value, 0) || v.value <= 0 || v.value > v.limit {
			return ErrInvalidInput
		}
	}
	if p.SingleWeight > p.IndustryWeight || p.IndustryWeight > p.StockWeight {
		return ErrInvalidInput
	}
	return nil
}

type AccountConfig struct {
	StrategyVersionID string     `json:"strategyVersionId"`
	UniverseVersionID string     `json:"universeVersionId"`
	Policy            RiskPolicy `json:"policy"`
	EffectiveAt       time.Time  `json:"effectiveAt"`
}
type Account struct {
	DatasetVersion                                       string
	ID, Name, Mode, StrategyVersionID, UniverseVersionID string
	Scope                                                Scope
	Balances                                             Balances
	InitialCash                                          Money
	Version                                              int
	AutomationEnabled                                    bool
	PauseReason                                          string
	Policy                                               RiskPolicy
	PendingConfig                                        *AccountConfig
	CreatedAt                                            time.Time
}

func ValidateAccountDataset(a Account, s Snapshot) error {
	if a.Mode != s.Mode || a.DatasetVersion == "" || a.DatasetVersion != s.DatasetVersion {
		return ErrVersionConflict
	}
	return nil
}

type LedgerEntry struct {
	ID, AccountID, EventKey, Kind, InstrumentID string
	Delta                                       Balances
	QuantityDelta                               Quantity
	EffectiveAt, RecordedAt                     time.Time
}
type Position struct {
	InstrumentID, Industry     string
	Quantity, ReservedQuantity Quantity
	Cost                       Money
}
