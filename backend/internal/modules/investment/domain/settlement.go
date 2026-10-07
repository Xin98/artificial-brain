package domain

import (
	"time"
)

func SettleReceivables(account Account, entries []LedgerEntry, calendar Calendar, now time.Time) (LedgerMutation, error) {
	out := LedgerMutation{Account: account, Entries: []LedgerEntry{}}
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Kind != "sell_fill" || entry.AccountID != account.ID || entry.Delta.Unsettled < 0 || entry.EventKey == "" || seen[entry.EventKey] {
			return out, ErrInvalidInput
		}
		seen[entry.EventKey] = true
		due, e := calendar.SettlementStart(entry.EffectiveAt)
		if e != nil {
			return out, e
		}
		if now.Before(due) {
			continue
		}
		delta := Balances{Available: entry.Delta.Unsettled, Unsettled: -entry.Delta.Unsettled}
		out.Account.Balances, e = out.Account.Balances.Apply(delta)
		if e != nil {
			return out, e
		}
		out.Entries = append(out.Entries, LedgerEntry{AccountID: account.ID, EventKey: "settlement/" + entry.EventKey, Kind: "settlement", InstrumentID: entry.InstrumentID, Delta: delta, EffectiveAt: due, RecordedAt: now})
	}
	return out, nil
}
