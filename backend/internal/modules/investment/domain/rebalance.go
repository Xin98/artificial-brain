package domain

import (
	"math/big"
	"sort"
)

type PlannedOrder struct {
	InstrumentID, Side, Reason string
	Quantity                   Quantity
}
type RebalancePlan struct {
	Orders  []PlannedOrder
	Pause   bool
	Reasons []string
}

func BuildRebalance(in PortfolioInput) (RebalancePlan, error) {
	out := RebalancePlan{Orders: []PlannedOrder{}, Reasons: []string{}}
	parameters := DefaultStrategyParameters()
	if in.Parameters != nil {
		parameters = *in.Parameters
	}
	if e := parameters.Validate(); e != nil {
		return out, e
	}
	if e := in.Policy.Validate(); e != nil {
		return out, e
	}
	if in.NAV <= 0 || in.PeakNAV < 0 || in.SessionTurnover < 0 {
		return out, ErrInvalidInput
	}
	if drawdownReached(in.NAV, in.PeakNAV, in.Policy.DrawdownPause) {
		out.Pause = true
		out.Reasons = append(out.Reasons, "drawdown_pause")
		return out, nil
	}
	for _, o := range in.Orders {
		if !o.Terminal() {
			out.Reasons = append(out.Reasons, "unresolved_orders")
			return out, nil
		}
	}
	prices, instruments, e := quoteBook(in.Snapshot, in.Prices)
	if e != nil {
		return out, e
	}
	positions := append([]Position(nil), in.Positions...)
	sort.Slice(positions, func(i, j int) bool { return positions[i].InstrumentID < positions[j].InstrumentID })
	signals := map[string]Signal{}
	ranked := append([]Signal(nil), in.Evaluation.Signals...)
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].InstrumentID < ranked[j].InstrumentID
	})
	for _, s := range ranked {
		signals[s.InstrumentID] = s
	}
	turnover := in.SessionTurnover
	cash := in.Account.Balances.Available
	blocked := map[string]bool{}
	// Risk reductions are projected first. Sale proceeds never increase today's settled buying power.
	appendSell := func(n int, qty Quantity, reason string) error {
		if qty <= 0 {
			return nil
		}
		p := &positions[n]
		decision, e := ValidateOrder(OrderRiskInput{Account: in.Account, Positions: positions, Snapshot: in.Snapshot, InstrumentID: p.InstrumentID, Side: "sell", Quantity: qty, Price: prices[p.InstrumentID], NAV: in.NAV, Policy: in.Policy, SessionTurnover: turnover, Reason: reason, Prices: in.Prices})
		if e != nil {
			return e
		}
		if qty > decision.MaxQuantity {
			qty = decision.MaxQuantity
		}
		if qty <= 0 {
			out.Reasons = append(out.Reasons, "turnover_budget_exhausted")
			return nil
		}
		gross, e := GrossValue(prices[p.InstrumentID], qty)
		if e != nil {
			return e
		}
		turnover, e = addMoney(turnover, gross)
		if e != nil {
			return e
		}
		out.Orders = append(out.Orders, PlannedOrder{p.InstrumentID, "sell", reason, qty})
		p.Quantity -= qty
		blocked[p.InstrumentID] = true
		return nil
	}
	// Stop losses precede ordinary score exits.
	for _, stop := range []bool{true, false} {
		for n, p := range positions {
			if p.Quantity <= 0 {
				continue
			}
			if p.ReservedQuantity != 0 {
				return out, ErrVersionConflict
			}
			price, ok := prices[p.InstrumentID]
			if !ok {
				return out, ErrDataStale
			}
			gross, e := GrossValue(price, p.Quantity)
			if e != nil {
				return out, e
			}
			loss := p.CostBasis > 0 && drawdownReached(gross, p.CostBasis, in.Policy.StopLoss)
			if stop && loss {
				if e = appendSell(n, p.Quantity, "stop_loss"); e != nil {
					return out, e
				}
			} else if !stop && !blocked[p.InstrumentID] {
				if signal, ok := signals[p.InstrumentID]; ok && signal.Score < parameters.ExitScore {
					if e = appendSell(n, p.Quantity, "exit_signal"); e != nil {
						return out, e
					}
				}
			}
		}
	}
	for _, kind := range []string{"reduce_single_risk", "reduce_industry_risk", "reduce_stock_risk"} {
		for n, p := range positions {
			if p.Quantity <= 0 {
				continue
			}
			ex, e := portfolioExposure(positions, nil, prices, instruments, "")
			if e != nil {
				return out, e
			}
			excess := Money(0)
			switch kind {
			case "reduce_single_risk":
				excess = ex.byID[p.InstrumentID] - moneyLimit(in.NAV, in.Policy.SingleWeight)
			case "reduce_industry_risk":
				excess = ex.byIndustry[ex.industries[p.InstrumentID]] - moneyLimit(in.NAV, in.Policy.IndustryWeight)
			case "reduce_stock_risk":
				excess = ex.stocks - moneyLimit(in.NAV, in.Policy.StockWeight)
			}
			if excess <= 0 {
				continue
			}
			keepValue := ex.byID[p.InstrumentID] - excess
			if keepValue < 0 {
				keepValue = 0
			}
			keep := maxWholeQuantity(p.Quantity, func(q Quantity) bool {
				v, e := GrossValue(prices[p.InstrumentID], q)
				return e == nil && v <= keepValue
			})
			if e = appendSell(n, p.Quantity-keep, kind); e != nil {
				return out, e
			}
		}
	}
	held := map[string]int{}
	holdings := 0
	for n, p := range positions {
		held[p.InstrumentID] = n
		if p.Quantity > 0 {
			holdings++
		}
	}
	for _, signal := range ranked {
		if signal.Score < parameters.EntryScore || signal.Risk.Level == "high" || signal.Risk.Level == "unknown" || blocked[signal.InstrumentID] {
			continue
		}
		price, ok := prices[signal.InstrumentID]
		if !ok {
			out.Reasons = append(out.Reasons, "candidate_price_missing")
			continue
		}
		index, exists := held[signal.InstrumentID]
		current := Quantity(0)
		if exists {
			current = positions[index].Quantity
		}
		if current == 0 && holdings >= parameters.MaxHoldings {
			continue
		}
		targetValue := moneyLimit(in.NAV, in.Policy.SingleWeight)
		currentValue, e := GrossValue(price, current)
		if e != nil && current > 0 {
			return out, e
		}
		// Compare in exact cents; a strict difference below 2% NAV is ignored.
		band := moneyLimit(in.NAV, parameters.RebalanceBand)
		difference := targetValue - currentValue
		if difference < band {
			continue
		}
		desired := maxWholeQuantity(1000000000, func(q Quantity) bool { gross, e := GrossValue(price, q); return e == nil && gross <= targetValue }) - current
		if desired <= 0 {
			continue
		}
		account := in.Account
		account.Balances.Available = cash
		decision, e := ValidateOrder(OrderRiskInput{Account: account, Positions: positions, Snapshot: in.Snapshot, Evaluation: in.Evaluation, InstrumentID: signal.InstrumentID, Side: "buy", Quantity: desired, Price: price, NAV: in.NAV, Policy: in.Policy, SessionTurnover: turnover, Automatic: true, Prices: in.Prices, Parameters: &parameters})
		if e != nil {
			return out, e
		}
		qty := desired
		if qty > decision.MaxQuantity {
			qty = decision.MaxQuantity
		}
		if qty <= 0 {
			continue
		}
		gross, e := GrossValue(price, qty)
		if e != nil {
			return out, e
		}
		cost, e := purchaseCost(price, qty)
		if e != nil {
			return out, e
		}
		cash -= cost
		turnover, e = addMoney(turnover, gross)
		if e != nil {
			return out, e
		}
		out.Orders = append(out.Orders, PlannedOrder{signal.InstrumentID, "buy", "entry_signal", qty})
		if !exists {
			index = len(positions)
			held[signal.InstrumentID] = index
			positions = append(positions, Position{InstrumentID: signal.InstrumentID, Industry: signal.Industry})
		}
		if positions[index].Quantity == 0 {
			holdings++
		}
		positions[index].Quantity += qty
	}
	if in.Evaluation.State != "completed" {
		out.Reasons = append(out.Reasons, "insufficient_quantitative_evidence")
	}
	return out, nil
}

// proportionalCost preserves the final cent on a full liquidation.
func proportionalCost(cost Money, qty, total Quantity) (Money, error) {
	if cost < 0 || qty < 0 || total <= 0 || qty > total {
		return 0, ErrInvalidInput
	}
	numerator := new(big.Int).Mul(big.NewInt(int64(cost)), big.NewInt(int64(qty)))
	den := big.NewInt(int64(total))
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(numerator, den, r)
	if new(big.Int).Mul(r, big.NewInt(2)).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, ErrOverflow
	}
	return Money(q.Int64()), nil
}
