package domain

import (
	"math/big"
	"time"
)

func roundedRatMoney(r *big.Rat) (Money, error) {
	if r.Sign() < 0 {
		return 0, ErrInvalidInput
	}
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	if new(big.Int).Mul(rem, big.NewInt(2)).Cmp(r.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, ErrOverflow
	}
	return Money(q.Int64()), nil
}
func ApplyCorporateAction(account Account, positions []Position, action CorporateAction, eligibility []Position) (LedgerMutation, error) {
	out := LedgerMutation{Account: account, Positions: append([]Position(nil), positions...), Entries: []LedgerEntry{}}
	if action.ID == "" || action.InstrumentID == "" || action.Currency != "USD" || action.EffectiveAt.IsZero() {
		return out, ErrCorporateActionIncomplete
	}
	entry := LedgerEntry{AccountID: account.ID, EventKey: "action/" + action.ID, Kind: action.Kind, InstrumentID: action.InstrumentID, EffectiveAt: action.EffectiveAt, RecordedAt: action.AvailableAt}
	switch action.Kind {
	case "split":
		if action.RatioNumerator <= 0 || action.RatioDenominator <= 0 {
			return out, ErrCorporateActionIncomplete
		}
		found := false
		for n, p := range out.Positions {
			if p.InstrumentID != action.InstrumentID {
				continue
			}
			if found || p.ReservedQuantity != 0 || p.Quantity < 0 || p.CostBasis < 0 {
				return out, ErrInvalidInput
			}
			found = true
			total := new(big.Int).Mul(big.NewInt(int64(p.Quantity)), big.NewInt(action.RatioNumerator))
			den := big.NewInt(action.RatioDenominator)
			whole, fraction := new(big.Int), new(big.Int)
			whole.QuoRem(total, den, fraction)
			if !whole.IsInt64() {
				return out, ErrOverflow
			}
			if fraction.Sign() != 0 {
				if action.CashInLieu == nil || *action.CashInLieu < 0 || action.CashInLieuUnit != "USD_per_post_split_share" {
					return out, ErrCorporateActionIncomplete
				}
				cash, e := roundedRatMoney(new(big.Rat).Mul(new(big.Rat).SetInt64(int64(*action.CashInLieu)), new(big.Rat).SetFrac(fraction, den)))
				if e != nil {
					return out, e
				}
				cost, e := roundedRatMoney(new(big.Rat).Mul(new(big.Rat).SetInt64(int64(p.CostBasis)), new(big.Rat).SetFrac(fraction, total)))
				if e != nil {
					return out, e
				}
				out.Positions[n].CostBasis -= cost
				entry.Delta.Available = cash
			}
			out.Positions[n].Quantity = Quantity(whole.Int64())
			entry.QuantityDelta = out.Positions[n].Quantity - p.Quantity
		}
	case "dividend":
		if action.Amount <= 0 || action.PayAt.IsZero() || action.PayAt.Before(action.EffectiveAt) {
			return out, ErrCorporateActionIncomplete
		}
		entry.Kind = "dividend_accrual"
		qty := Quantity(0)
		for _, p := range eligibility {
			if p.InstrumentID == action.InstrumentID {
				if p.Quantity < 0 {
					return out, ErrInvalidInput
				}
				sum, e := addMoney(Money(qty), Money(p.Quantity))
				if e != nil {
					return out, e
				}
				qty = Quantity(sum)
			}
		}
		if qty > 0 {
			amount, e := GrossValue(action.Amount, qty)
			if e != nil {
				return out, e
			}
			entry.Delta.Dividends = amount
		}
	default:
		return out, ErrCorporateActionIncomplete
	}
	var e error
	out.Account.Balances, e = account.Balances.Apply(entry.Delta)
	if e != nil {
		return out, e
	}
	out.Entries = append(out.Entries, entry)
	return out, nil
}
func PayDividend(account Account, action CorporateAction, accrual LedgerEntry, now time.Time) (LedgerMutation, error) {
	out := LedgerMutation{Account: account, Entries: []LedgerEntry{}}
	if action.PayAt.IsZero() || now.Before(action.PayAt) || accrual.AccountID != account.ID || accrual.EventKey != "action/"+action.ID || accrual.Delta.Dividends < 0 {
		return out, ErrCorporateActionIncomplete
	}
	delta := Balances{Available: accrual.Delta.Dividends, Dividends: -accrual.Delta.Dividends}
	var e error
	out.Account.Balances, e = account.Balances.Apply(delta)
	if e != nil {
		return out, e
	}
	out.Entries = append(out.Entries, LedgerEntry{AccountID: account.ID, EventKey: "payment/" + action.ID, Kind: "dividend_payment", InstrumentID: action.InstrumentID, Delta: delta, EffectiveAt: action.PayAt, RecordedAt: now})
	return out, nil
}
func SameActionEconomics(a, b CorporateAction) bool {
	sameCash := a.CashInLieu == nil && b.CashInLieu == nil
	if a.CashInLieu != nil && b.CashInLieu != nil {
		sameCash = *a.CashInLieu == *b.CashInLieu
	}
	return a.ID == b.ID && a.InstrumentID == b.InstrumentID && a.Kind == b.Kind && a.Currency == b.Currency && a.EffectiveAt.Equal(b.EffectiveAt) && a.PayAt.Equal(b.PayAt) && a.RatioNumerator == b.RatioNumerator && a.RatioDenominator == b.RatioDenominator && a.Amount == b.Amount && sameCash && a.CashInLieuUnit == b.CashInLieuUnit
}
