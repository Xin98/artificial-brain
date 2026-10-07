package domain

import (
	"sort"
	"time"
)

type LedgerMutation struct {
	Account   Account
	Positions []Position
	Entries   []LedgerEntry
}

func ApplyFill(account Account, positions []Position, fill Fill) (LedgerMutation, error) {
	out := LedgerMutation{Account: account, Positions: append([]Position(nil), positions...), Entries: []LedgerEntry{}}
	if fill.ID == "" || fill.OrderID == "" || fill.AccountID != account.ID || fill.Quantity <= 0 || fill.Price <= 0 || fill.Fee <= 0 || fill.ReservedCash < 0 || fill.ReservedQuantity < 0 || (fill.Side != "buy" && fill.Side != "sell") {
		return out, ErrInvalidInput
	}
	gross, e := GrossValue(fill.Price, fill.Quantity)
	if e != nil {
		return out, e
	}
	fee, e := TradeFee(gross)
	if e != nil || gross != fill.Gross || fee != fill.Fee {
		return out, ErrInvalidInput
	}
	index := -1
	for n, p := range out.Positions {
		if p.InstrumentID == fill.InstrumentID {
			if index >= 0 {
				return out, ErrInvalidInput
			}
			index = n
		}
	}
	if index < 0 {
		if fill.Side == "sell" {
			return out, ErrInsufficientCash
		}
		index = len(out.Positions)
		out.Positions = append(out.Positions, Position{InstrumentID: fill.InstrumentID})
	}
	p := &out.Positions[index]
	delta := Balances{}
	quantityDelta := fill.Quantity
	if fill.Side == "buy" {
		cost, e := addMoney(fill.Gross, fill.Fee)
		if e != nil {
			return out, e
		}
		if cost > fill.ReservedCash {
			return out, ErrInsufficientCash
		}
		delta = Balances{Available: fill.ReservedCash - cost, Reserved: -fill.ReservedCash}
		qty, e := addMoney(Money(p.Quantity), Money(fill.Quantity))
		if e != nil {
			return out, e
		}
		p.Quantity = Quantity(qty)
		p.CostBasis, e = addMoney(p.CostBasis, cost)
		if e != nil {
			return out, e
		}
	} else {
		if p.Quantity < fill.Quantity || p.ReservedQuantity < fill.ReservedQuantity || fill.ReservedQuantity < fill.Quantity || fill.Gross < fill.Fee {
			return out, ErrInsufficientCash
		}
		cost, e := proportionalCost(p.CostBasis, fill.Quantity, p.Quantity)
		if e != nil {
			return out, e
		}
		p.CostBasis -= cost
		p.Quantity -= fill.Quantity
		p.ReservedQuantity -= fill.ReservedQuantity
		delta.Unsettled = fill.Gross - fill.Fee
		quantityDelta = -fill.Quantity
	}
	out.Account.Balances, e = account.Balances.Apply(delta)
	if e != nil {
		return out, e
	}
	out.Entries = append(out.Entries, LedgerEntry{ID: fill.ID, AccountID: account.ID, EventKey: "fill/" + fill.OrderID, Kind: fill.Side + "_fill", InstrumentID: fill.InstrumentID, Delta: delta, QuantityDelta: quantityDelta, EffectiveAt: fill.EffectiveAt, RecordedAt: fill.RecordedAt})
	sort.Slice(out.Positions, func(i, j int) bool { return out.Positions[i].InstrumentID < out.Positions[j].InstrumentID })
	return out, nil
}
func ReleaseOrder(account Account, positions []Position, o Order, kind string, now time.Time) (LedgerMutation, error) {
	out := LedgerMutation{Account: account, Positions: append([]Position(nil), positions...), Entries: []LedgerEntry{}}
	if o.AccountID != account.ID || o.Terminal() || o.ReservedCash < 0 || o.ReservedQuantity < 0 {
		return out, ErrInvalidInput
	}
	delta := Balances{}
	switch o.Side {
	case "buy":
		delta = Balances{Available: o.ReservedCash, Reserved: -o.ReservedCash}
	case "sell":
		found := false
		for n, p := range out.Positions {
			if p.InstrumentID == o.InstrumentID {
				if p.ReservedQuantity < o.ReservedQuantity {
					return out, ErrInvalidInput
				}
				out.Positions[n].ReservedQuantity -= o.ReservedQuantity
				found = true
			}
		}
		if !found {
			return out, ErrInvalidInput
		}
	default:
		return out, ErrInvalidInput
	}
	var e error
	out.Account.Balances, e = account.Balances.Apply(delta)
	if e != nil {
		return out, e
	}
	out.Entries = append(out.Entries, LedgerEntry{AccountID: account.ID, EventKey: "release/" + o.ID, Kind: kind, InstrumentID: o.InstrumentID, Delta: delta, EffectiveAt: now, RecordedAt: now})
	return out, nil
}
