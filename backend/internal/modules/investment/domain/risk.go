package domain

import (
	"math/big"
	"strconv"
	"time"
)

type PortfolioInput struct {
	Account                       Account
	Positions                     []Position
	Orders                        []Order
	Snapshot                      Snapshot
	Evaluation                    Evaluation
	Policy                        RiskPolicy
	NAV, PeakNAV, SessionTurnover Money
	Prices                        map[string]Price
	Parameters                    *StrategyParameters
}
type OrderRiskInput struct {
	Account                                    Account
	Positions                                  []Position
	Orders                                     []Order
	Snapshot                                   Snapshot
	Evaluation                                 Evaluation
	InstrumentID, Side, Reason, ExcludeOrderID string
	Quantity                                   Quantity
	Price                                      Price
	NAV                                        Money
	Policy                                     RiskPolicy
	SessionTurnover                            Money
	Automatic                                  bool
	ReservationBudget                          *Money
	Prices                                     map[string]Price
	Parameters                                 *StrategyParameters
}
type RiskDecision struct {
	Allowed     bool
	ReasonCode  string
	MaxQuantity Quantity
}
type exposure struct {
	stocks           Money
	byID, byIndustry map[string]Money
	industries       map[string]string
}

func policyFraction(v float64) *big.Rat {
	r, _ := new(big.Rat).SetString(strconv.FormatFloat(v, 'f', -1, 64))
	return r
}
func moneyLimit(nav Money, weight float64) Money {
	r := new(big.Rat).Mul(new(big.Rat).SetInt64(int64(nav)), policyFraction(weight))
	return Money(new(big.Int).Quo(r.Num(), r.Denom()).Int64())
}
func drawdownReached(nav, peak Money, threshold float64) bool {
	if peak <= 0 {
		return false
	}
	remaining := new(big.Rat).Sub(big.NewRat(1, 1), policyFraction(threshold))
	return new(big.Rat).SetFrac64(int64(nav), int64(peak)).Cmp(remaining) <= 0
}
func quoteBook(s Snapshot, overrides map[string]Price) (map[string]Price, map[string]Instrument, error) {
	prices := map[string]Price{}
	inst := map[string]Instrument{}
	latest, e := s.Calendar.LatestCompleted(s.AsOf)
	if e != nil {
		return nil, nil, e
	}
	for _, i := range s.Instruments {
		inst[i.ID] = i
	}
	for _, b := range s.Bars {
		if !b.AvailableAt.After(s.AsOf) && b.SessionDate.Equal(latest.Date) && b.Close > 0 {
			prices[b.InstrumentID] = b.Close
		}
	}
	for id, p := range overrides {
		if p <= 0 {
			return nil, nil, ErrInvalidInput
		}
		prices[id] = p
	}
	return prices, inst, nil
}
func portfolioExposure(positions []Position, orders []Order, prices map[string]Price, instruments map[string]Instrument, exclude string) (exposure, error) {
	out := exposure{byID: map[string]Money{}, byIndustry: map[string]Money{}, industries: map[string]string{}}
	add := func(id string, v Money) error {
		i, ok := instruments[id]
		if !ok {
			return ErrDataStale
		}
		industry := Industry(i.SIC)
		if industry == "unknown" {
			return ErrFactorUnavailable
		}
		var e error
		out.stocks, e = addMoney(out.stocks, v)
		if e != nil {
			return e
		}
		out.byID[id], e = addMoney(out.byID[id], v)
		if e != nil {
			return e
		}
		out.byIndustry[industry], e = addMoney(out.byIndustry[industry], v)
		if e != nil {
			return e
		}
		out.industries[id] = industry
		return nil
	}
	seen := map[string]bool{}
	for _, pos := range positions {
		if seen[pos.InstrumentID] || pos.Quantity < 0 || pos.ReservedQuantity < 0 || pos.ReservedQuantity > pos.Quantity || pos.CostBasis < 0 {
			return out, ErrInvalidInput
		}
		seen[pos.InstrumentID] = true
		if pos.Quantity == 0 {
			continue
		}
		price, ok := prices[pos.InstrumentID]
		if !ok {
			return out, ErrDataStale
		}
		v, e := GrossValue(price, pos.Quantity)
		if e != nil {
			return out, e
		}
		if e = add(pos.InstrumentID, v); e != nil {
			return out, e
		}
	}
	for _, o := range orders {
		if o.ID == exclude || o.Terminal() {
			continue
		}
		if o.State != "pending" && o.State != "awaiting_bar" {
			return out, ErrInvalidInput
		}
		if o.Side != "buy" {
			continue
		}
		if o.ReservedCash < 0 {
			return out, ErrInvalidInput
		}
		if e := add(o.InstrumentID, o.ReservedCash); e != nil {
			return out, e
		}
	}
	return out, nil
}
func TradeFee(gross Money) (Money, error) {
	if gross < 0 {
		return 0, ErrInvalidInput
	}
	fee := gross / 10000
	if gross%10000 != 0 {
		fee++
	}
	if fee < 1 {
		fee = 1
	}
	return fee, nil
}
func purchaseCost(price Price, qty Quantity) (Money, error) {
	gross, e := GrossValue(price, qty)
	if e != nil {
		return 0, e
	}
	fee, e := TradeFee(gross)
	if e != nil {
		return 0, e
	}
	return addMoney(gross, fee)
}
func maxWholeQuantity(limit Quantity, fits func(Quantity) bool) Quantity {
	low, high := Quantity(0), limit
	for low < high {
		mid := low + (high-low+1)/2
		if fits(mid) {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}
func emergencySell(reason string) bool {
	return reason == "stop_loss" || reason == "reduce_single_risk" || reason == "reduce_industry_risk" || reason == "reduce_stock_risk"
}
func ValidateOrder(in OrderRiskInput) (RiskDecision, error) {
	out := RiskDecision{ReasonCode: "risk_limit_exceeded"}
	if in.Quantity <= 0 || in.Quantity > 1000000000 || in.Price <= 0 || in.NAV <= 0 || in.SessionTurnover < 0 || (in.Side != "buy" && in.Side != "sell") {
		return out, ErrInvalidInput
	}
	if e := in.Policy.Validate(); e != nil {
		return out, e
	}
	prices, instruments, e := quoteBook(in.Snapshot, in.Prices)
	if e != nil {
		return out, e
	}
	prices[in.InstrumentID] = in.Price
	instrument, ok := instruments[in.InstrumentID]
	if !ok || instrument.Kind != "stock" || !instrument.Tradable {
		out.ReasonCode = "instrument_ineligible"
		return out, nil
	}
	industry := Industry(instrument.SIC)
	if industry == "unknown" {
		out.ReasonCode = "factor_unavailable"
		return out, nil
	}
	turnover := moneyLimit(in.NAV, in.Policy.TurnoverLimit) - in.SessionTurnover
	if turnover < 0 {
		turnover = 0
	}
	if in.Side == "sell" {
		available := Quantity(0)
		for _, p := range in.Positions {
			if p.InstrumentID == in.InstrumentID {
				if p.ReservedQuantity < 0 || p.ReservedQuantity > p.Quantity {
					return out, ErrInvalidInput
				}
				available = p.Quantity - p.ReservedQuantity
			}
		}
		if in.ExcludeOrderID != "" {
			for _, o := range in.Orders {
				if o.ID == in.ExcludeOrderID && o.Side == "sell" && !o.Terminal() {
					available += o.ReservedQuantity
				}
			}
		}
		if available < 0 {
			return out, ErrInvalidInput
		}
		if available > 1000000000 {
			available = 1000000000
		}
		out.MaxQuantity = maxWholeQuantity(available, func(q Quantity) bool {
			if emergencySell(in.Reason) {
				return true
			}
			gross, e := GrossValue(in.Price, q)
			return e == nil && gross <= turnover
		})
	} else {
		if in.Automatic {
			parameters := DefaultStrategyParameters()
			if in.Parameters != nil {
				parameters = *in.Parameters
			}
			if e := parameters.Validate(); e != nil {
				return out, e
			}
			eligible := false
			for _, s := range in.Evaluation.Signals {
				if s.InstrumentID == in.InstrumentID && s.Score >= parameters.EntryScore && s.Rank <= parameters.MaxHoldings && s.Risk.Level != "high" && s.Risk.Level != "unknown" {
					eligible = true
				}
			}
			if !eligible {
				out.ReasonCode = "automatic_candidate_ineligible"
				return out, nil
			}
		}
		exposure, e := portfolioExposure(in.Positions, in.Orders, prices, instruments, in.ExcludeOrderID)
		if e != nil {
			return out, e
		}
		cash := in.Account.Balances.Available
		if in.ReservationBudget != nil {
			cash = *in.ReservationBudget
		}
		if cash < 0 {
			return out, ErrInvalidInput
		}
		single := moneyLimit(in.NAV, in.Policy.SingleWeight) - exposure.byID[in.InstrumentID]
		sector := moneyLimit(in.NAV, in.Policy.IndustryWeight) - exposure.byIndustry[industry]
		stocks := moneyLimit(in.NAV, in.Policy.StockWeight) - exposure.stocks
		out.MaxQuantity = maxWholeQuantity(1000000000, func(q Quantity) bool {
			gross, e := GrossValue(in.Price, q)
			if e != nil || gross > single || gross > sector || gross > stocks || gross > turnover {
				return false
			}
			cost, e := purchaseCost(in.Price, q)
			return e == nil && cost <= cash
		})
		if out.MaxQuantity == 0 && cash <= 0 {
			out.ReasonCode = "insufficient_settled_cash"
		}
	}
	out.Allowed = in.Quantity <= out.MaxQuantity
	if out.Allowed {
		out.ReasonCode = ""
	}
	return out, nil
}
func ComputeNAV(account Account, positions []Position, snapshot Snapshot, overrides map[string]Price) (Money, error) {
	total, e := account.Balances.Total()
	if e != nil {
		return 0, e
	}
	prices, _, e := quoteBook(snapshot, overrides)
	if e != nil {
		return 0, e
	}
	for _, p := range positions {
		if p.Quantity == 0 {
			continue
		}
		price, ok := prices[p.InstrumentID]
		if !ok {
			return 0, ErrDataStale
		}
		value, e := GrossValue(price, p.Quantity)
		if e != nil {
			return 0, e
		}
		total, e = addMoney(total, value)
		if e != nil {
			return 0, e
		}
	}
	return total, nil
}
func dateUTC(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}
