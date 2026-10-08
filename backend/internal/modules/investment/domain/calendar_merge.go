package domain

import (
	"sort"
	"time"
)

// MergeCalendar preserves history while allowing an overlapping provider refresh to replace changed sessions.
func MergeCalendar(old, next Calendar) (Calendar, error) {
	if old.CoverageStart.IsZero() {
		return next, nil
	}
	if next.CoverageStart.After(old.CoverageEnd.Add(time.Nanosecond)) || old.CoverageStart.After(next.CoverageEnd.Add(time.Nanosecond)) {
		return Calendar{}, ErrDataStale
	}
	out := next
	if old.CoverageStart.Before(out.CoverageStart) {
		out.CoverageStart = old.CoverageStart
	}
	if old.CoverageEnd.After(out.CoverageEnd) {
		out.CoverageEnd = old.CoverageEnd
	}
	byDay := map[string]Session{}
	for _, s := range old.Sessions {
		if s.Date.Before(next.CoverageStart) || s.Date.After(next.CoverageEnd) {
			byDay[s.Date.UTC().Format("2006-01-02")] = s
		}
	}
	for _, s := range next.Sessions {
		byDay[s.Date.UTC().Format("2006-01-02")] = s
	}
	out.Sessions = []Session{}
	for _, s := range byDay {
		out.Sessions = append(out.Sessions, s)
	}
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].Date.Before(out.Sessions[j].Date) })
	// An absent settlement manifest remains absent; do not resurrect yesterday's proof after it was removed.
	out.SettlementDays = []time.Time{}
	if len(next.SettlementDays) > 0 {
		days := map[string]time.Time{}
		for _, d := range old.SettlementDays {
			if d.Before(next.CoverageStart) || d.After(next.CoverageEnd) {
				days[d.Format("2006-01-02")] = d
			}
		}
		for _, d := range next.SettlementDays {
			days[d.Format("2006-01-02")] = d
		}
		for _, d := range days {
			out.SettlementDays = append(out.SettlementDays, d)
		}
		sort.Slice(out.SettlementDays, func(i, j int) bool { return out.SettlementDays[i].Before(out.SettlementDays[j]) })
	}
	return out, nil
}
