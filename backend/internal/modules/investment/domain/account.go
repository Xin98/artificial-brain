package domain

import "time"

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

type RiskPolicy struct{ SingleWeight, IndustryWeight, StockWeight, DrawdownPause, StopLoss, TurnoverLimit float64 }

func DefaultRiskPolicy() RiskPolicy { return RiskPolicy{.1, .3, .8, .15, .1, .2} }

type AccountConfig struct {
	StrategyVersionID, UniverseVersionID string
	Policy                               RiskPolicy
	EffectiveAt                          time.Time
}
type Account struct {
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
