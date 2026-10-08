package domain

import (
	"context"
	"sort"
	"time"
)

// BenchmarkSeries starts at the first opening after account creation and uses the paper execution cost and dividend rules.
func BenchmarkSeries(ctx context.Context, source Snapshot, created time.Time, points []NAVPoint, initial Money) ([]NAVPoint, Performance, error) {
	out := []NAVPoint{}
	if len(points) == 0 || initial <= 0 {
		return out, Performance{}, ErrDataStale
	}
	for _, f := range source.QualityFlags {
		if f == "corporate_action_publication_time_unknown" || f == "corporate_action_history_incomplete" {
			return out, Performance{}, ErrCorporateActionIncomplete
		}
	}
	id := ""
	for _, i := range source.Instruments {
		if i.Ticker == "SPY" {
			id = i.ID
		}
	}
	if id == "" {
		return out, Performance{}, ErrDataStale
	}
	start, e := source.Calendar.NextSession(created)
	if e != nil {
		return out, Performance{}, e
	}
	wanted := map[string]bool{}
	last := points[len(points)-1].SessionDate
	for _, p := range points {
		wanted[p.SessionDate.Format("2006-01-02")] = true
		if p.SessionDate.Before(start.Date) {
			out = append(out, NAVPoint{SessionDate: p.SessionDate, NAV: initial})
		}
	}
	in := BacktestInput{Snapshots: []Snapshot{source}, InitialCash: initial}
	book := newReplayBook("benchmark", in)
	sessions := append([]Session(nil), source.Calendar.Sessions...)
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Date.Before(sessions[j].Date) })
	bought := false
	for _, session := range sessions {
		if session.Date.Before(start.Date) || session.Date.After(last) {
			continue
		}
		if e = ctx.Err(); e != nil {
			return out, Performance{}, e
		}
		now := session.CloseAt.Add(30 * time.Minute)
		current, e := SelectSnapshot(source, now)
		if e != nil {
			return out, Performance{}, e
		}
		if e = book.reconcile(current, session.OpenAt, map[string]bool{id: true}); e != nil {
			return out, Performance{}, e
		}
		if !bought {
			prior, e := SelectSnapshot(source, session.OpenAt.Add(-time.Nanosecond))
			if e != nil {
				return out, Performance{}, e
			}
			if e = book.buyBenchmark(prior, current, session, now, id); e != nil {
				return out, Performance{}, e
			}
			bought = true
		}
		if e = book.reconcile(current, now, map[string]bool{id: true}); e != nil {
			return out, Performance{}, e
		}
		if wanted[session.Date.Format("2006-01-02")] {
			nav, e := ComputeNAV(book.account, book.positions, current, nil)
			if e != nil {
				return out, Performance{}, e
			}
			out = append(out, NAVPoint{SessionDate: session.Date, NAV: nav})
		}
	}
	if len(out) != len(points) {
		return nil, Performance{}, ErrDataStale
	}
	p, e := ComputePerformance(out, book.fills, initial)
	return out, p, e
}
