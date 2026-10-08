package fixture_test

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	d "github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"reflect"
	"testing"
	"time"
)

func replayInput(t *testing.T) d.BacktestInput {
	t.Helper()
	a, e := fixture.New()
	if e != nil {
		t.Fatal(e)
	}
	u := d.UniverseVersion{ID: "pool", Mode: "fixture"}
	for _, i := range a.Snapshot.Instruments {
		if i.Kind == "stock" {
			u.InstrumentIDs = append(u.InstrumentIDs, i.ID)
		}
	}
	return d.BacktestInput{Snapshots: []d.Snapshot{a.Snapshot}, Calendar: a.Snapshot.Calendar, Universe: u, Strategy: d.StrategyVersion{ID: "strategy", Parameters: d.DefaultStrategyParameters()}, InitialCash: 10000000, From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), BenchmarkID: "fixture-spy"}
}
func TestReplayCannotUseNextCloseOrFutureFacts(t *testing.T) {
	in := replayInput(t)
	original, e := d.Replay(in)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := fixture.New()
	changed := a.Snapshot
	for n := range changed.Bars {
		b := &changed.Bars[n]
		if b.SessionDate.Equal(in.From) {
			b.Close = b.Open * 2
			b.High = b.Open * 3
			b.Low = b.Open / 2
			b.Volume *= 100
		}
	}
	for n := range changed.Facts {
		f := &changed.Facts[n]
		if f.AvailableAt.After(in.From) {
			f.Value = "999999999999"
		}
	}
	in.Snapshots = []d.Snapshot{changed}
	later, e := d.Replay(in)
	if e != nil {
		t.Fatal(e)
	}
	first := func(r d.BacktestResult) []d.ReplayEvent {
		out := []d.ReplayEvent{}
		for _, v := range r.Trace {
			if v.Fill != nil && v.Fill.EffectiveAt.UTC().Format("2006-01-02") == in.From.Format("2006-01-02") {
				out = append(out, v)
			}
		}
		return out
	}
	if len(first(original)) == 0 || !reflect.DeepEqual(first(original), first(later)) {
		t.Fatal("future close/volume/facts altered opening execution")
	}
}
func TestRejectIncompleteHistory(t *testing.T) {
	in := replayInput(t)
	in.To = in.From.AddDate(0, 0, 5)
	if _, e := d.Replay(in); e == nil {
		t.Fatal("short experiment")
	}
	in = replayInput(t)
	in.Snapshots[0].QualityFlags = append(in.Snapshots[0].QualityFlags, "corporate_action_publication_time_unknown")
	if _, e := d.Replay(in); e == nil {
		t.Fatal("unproven actions")
	}
	in = replayInput(t)
	bars := []d.Bar{}
	for _, b := range in.Snapshots[0].Bars {
		if !b.SessionDate.Before(in.From.AddDate(0, -2, 0)) {
			bars = append(bars, b)
		}
	}
	in.Snapshots[0].Bars = bars
	if _, e := d.Replay(in); e == nil {
		t.Fatal("short warmup")
	}
}
func TestReplayLateBarKeepsOriginalEffectiveDate(t *testing.T) {
	in := replayInput(t)
	first, _ := in.Calendar.Session(in.From)
	next, _ := in.Calendar.NextSession(first.CloseAt)
	for n := range in.Snapshots[0].Bars {
		v := &in.Snapshots[0].Bars[n]
		if v.SessionDate.Equal(first.Date) && v.InstrumentID != "fixture-spy" {
			v.AvailableAt = next.CloseAt.Add(-time.Minute)
		}
	}
	r, e := d.Replay(in)
	if e != nil {
		t.Fatal(e)
	}
	late := 0
	for _, f := range r.Fills {
		if f.EffectiveAt.Equal(first.OpenAt) && f.RecordedAt.After(next.CloseAt) {
			late++
		}
	}
	if late == 0 {
		t.Fatal("late target bar was discarded instead of confirming the original opening")
	}
}
func TestBenchmarkSameFeesAndDividendBasis(t *testing.T) {
	in := replayInput(t)
	in.From = time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	in.To = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	r, e := d.Replay(in)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.BenchmarkCurve) != len(r.Curve) || len(r.BenchmarkFills) != 1 {
		t.Fatal("missing benchmark")
	}
	f := r.BenchmarkFills[0]
	fee, _ := d.TradeFee(f.Gross)
	if f.Fee != fee || f.Fee == 0 {
		t.Fatal(f)
	}
	dividend := false
	for _, v := range r.BenchmarkTrace {
		for _, l := range v.Entries {
			if l.Kind == "dividend_accrual" && l.Delta.Dividends > 0 {
				dividend = true
			}
		}
	}
	if !dividend {
		t.Fatal("benchmark lacks dividend total return")
	}
	again, e := d.Replay(in)
	if e != nil || !reflect.DeepEqual(r, again) {
		t.Fatal("nondeterministic replay", e)
	}
}
