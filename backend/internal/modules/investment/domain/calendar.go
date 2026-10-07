package domain

import (
	"sort"
	"time"
)

type Session struct{ Date, OpenAt, CloseAt time.Time }
type Calendar struct {
	Version, Source            string
	CoverageStart, CoverageEnd time.Time
	Sessions                   []Session
	SettlementDays             []time.Time
}

func (c Calendar) covered(t time.Time) bool {
	return !t.Before(c.CoverageStart) && !t.After(c.CoverageEnd)
}
func (c Calendar) NextSession(after time.Time) (Session, error) {
	if !c.covered(after) {
		return Session{}, ErrDataStale
	}
	sessions := append([]Session(nil), c.Sessions...)
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].OpenAt.Before(sessions[j].OpenAt) })
	for _, s := range sessions {
		if s.OpenAt.After(after) {
			return s, nil
		}
	}
	return Session{}, ErrDataStale
}
func (c Calendar) NextSettlement(after time.Time) (time.Time, error) {
	if !c.covered(after) {
		return time.Time{}, ErrDataStale
	}
	date := time.Date(after.UTC().Year(), after.UTC().Month(), after.UTC().Day(), 0, 0, 0, 0, time.UTC)
	days := append([]time.Time(nil), c.SettlementDays...)
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	for _, d := range days {
		if d.After(date) {
			return d, nil
		}
	}
	return time.Time{}, ErrDataStale
}
func (c Calendar) Session(date time.Time) (Session, error) {
	for _, s := range c.Sessions {
		if s.Date.Format("2006-01-02") == date.Format("2006-01-02") {
			return s, nil
		}
	}
	return Session{}, ErrDataStale
}
func (c Calendar) LatestCompleted(now time.Time) (Session, error) {
	if !c.covered(now) {
		return Session{}, ErrDataStale
	}
	var result Session
	for _, s := range c.Sessions {
		if !s.CloseAt.After(now) && s.CloseAt.After(result.CloseAt) {
			result = s
		}
	}
	if result.Date.IsZero() {
		return Session{}, ErrDataStale
	}
	return result, nil
}
