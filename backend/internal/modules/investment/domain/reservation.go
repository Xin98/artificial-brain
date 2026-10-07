package domain

func ReserveOrders(account Account, positions []Position, orders []Order) (LedgerMutation, error) {
	out := LedgerMutation{Account: account, Positions: append([]Position(nil), positions...), Entries: []LedgerEntry{}}
	seen := map[string]bool{}
	for _, o := range orders {
		if o.ID == "" || seen[o.ID] || o.AccountID != account.ID || o.State != OrderPending || o.Quantity <= 0 || o.Quantity > 1000000000 || o.Capacity < 0 || !o.TargetOpenAt.After(o.CreatedAt) || !o.ExpiresAt.After(o.TargetOpenAt) {
			return out, ErrInvalidInput
		}
		seen[o.ID] = true
		delta := Balances{}
		kind := "reserve_" + o.Side
		switch o.Side {
		case "buy":
			if o.ReservedCash <= 0 || o.ReservedQuantity != 0 {
				return out, ErrInvalidInput
			}
			delta = Balances{Available: -o.ReservedCash, Reserved: o.ReservedCash}
		case "sell":
			if o.ReservedCash != 0 || o.ReservedQuantity != o.Quantity {
				return out, ErrInvalidInput
			}
			found := false
			for n, p := range out.Positions {
				if p.InstrumentID == o.InstrumentID {
					if p.Quantity-p.ReservedQuantity < o.Quantity {
						return out, ErrInsufficientCash
					}
					out.Positions[n].ReservedQuantity += o.Quantity
					found = true
				}
			}
			if !found {
				return out, ErrInsufficientCash
			}
		default:
			return out, ErrInvalidInput
		}
		var e error
		out.Account.Balances, e = out.Account.Balances.Apply(delta)
		if e != nil {
			return out, e
		}
		out.Entries = append(out.Entries, LedgerEntry{AccountID: account.ID, EventKey: "reserve/" + o.ID, Kind: kind, InstrumentID: o.InstrumentID, Delta: delta, EffectiveAt: o.CreatedAt, RecordedAt: o.CreatedAt})
	}
	return out, nil
}
