package domain

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}
func TestSnapshotDoesNotExposeFutureRevision(t *testing.T) {
	s := Snapshot{Facts: []FinancialFact{{InstrumentID: "a", Concept: "Revenue", PeriodEnd: at("2025-12-31T00:00:00Z"), AvailableAt: at("2026-01-02T09:00:00Z"), Value: "100"}, {InstrumentID: "a", Concept: "Revenue", PeriodEnd: at("2025-12-31T00:00:00Z"), AvailableAt: at("2026-01-02T17:00:00Z"), Value: "200"}}}
	before, e := SelectSnapshot(s, at("2026-01-02T16:30:00Z"))
	if e != nil || len(before.Facts) != 1 || before.Facts[0].Value != "100" {
		t.Fatal(before, e)
	}
	after, _ := SelectSnapshot(s, at("2026-01-02T18:00:00Z"))
	if after.Facts[0].Value != "200" {
		t.Fatal(after)
	}
}
func TestCalendarSeparateSettlementAndTradingDays(t *testing.T) {
	c := Calendar{CoverageStart: at("2026-01-01T00:00:00Z"), CoverageEnd: at("2026-12-31T23:59:59Z"), Sessions: []Session{{Date: at("2026-10-12T00:00:00Z"), OpenAt: at("2026-10-12T13:30:00Z"), CloseAt: at("2026-10-12T20:00:00Z")}}, SettlementDays: []time.Time{at("2026-10-13T00:00:00Z")}}
	s, e := c.NextSession(at("2026-10-11T00:00:00Z"))
	d, se := c.NextSettlement(at("2026-10-11T00:00:00Z"))
	if e != nil || se != nil || s.Date.Day() != 12 || d.Day() != 13 {
		t.Fatal(s, d, e, se)
	}
	if _, e = c.NextSession(at("2027-01-01T00:00:00Z")); e == nil {
		t.Fatal("missing coverage")
	}
}
