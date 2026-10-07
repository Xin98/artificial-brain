package domain

import (
	"fmt"
	"sort"
	"time"
)

func SelectSnapshot(input Snapshot, asOf time.Time) (Snapshot, error) {
	out := input
	out.AsOf = asOf
	out.Bars = nil
	out.Facts = nil
	out.Actions = nil
	out.News = nil
	bars := map[string]Bar{}
	for _, b := range input.Bars {
		if !b.AvailableAt.After(asOf) {
			key := b.InstrumentID + "/" + b.SessionDate.Format("2006-01-02")
			old, ok := bars[key]
			if !ok || b.IngestedAt.After(old.IngestedAt) {
				bars[key] = b
			}
		}
	}
	for _, b := range bars {
		out.Bars = append(out.Bars, b)
	}
	facts := map[string]FinancialFact{}
	for _, f := range input.Facts {
		if f.AvailableAt.After(asOf) {
			continue
		}
		key := fmt.Sprintf("%s/%s/%s/%s/%s/%s", f.InstrumentID, f.Concept, f.PeriodStart.Format("2006-01-02"), f.PeriodEnd.Format("2006-01-02"), f.Unit, f.Currency)
		old, ok := facts[key]
		if !ok || f.AvailableAt.After(old.AvailableAt) || (f.AvailableAt.Equal(old.AvailableAt) && f.Accession > old.Accession) {
			facts[key] = f
		}
	}
	// Format the existing deterministic sort key once per fact, rather than
	// allocating time/string representations for every O(n log n) comparison.
	type orderedFact struct {
		key  string
		fact FinancialFact
	}
	orderedFacts := make([]orderedFact, 0, len(facts))
	for _, f := range facts {
		orderedFacts = append(orderedFacts, orderedFact{fmt.Sprint(f.InstrumentID, f.Concept, f.PeriodEnd, f.PeriodStart, f.Unit, f.Currency), f})
	}
	sort.Slice(orderedFacts, func(i, j int) bool { return orderedFacts[i].key < orderedFacts[j].key })
	out.Facts = make([]FinancialFact, 0, len(orderedFacts))
	for _, f := range orderedFacts {
		out.Facts = append(out.Facts, f.fact)
	}
	actions := map[string]CorporateAction{}
	for _, a := range input.Actions {
		if a.AvailableAt.After(asOf) {
			continue
		}
		old, ok := actions[a.ID]
		if !ok || a.AvailableAt.After(old.AvailableAt) || a.IngestedAt.After(old.IngestedAt) {
			actions[a.ID] = a
		}
	}
	for _, a := range actions {
		out.Actions = append(out.Actions, a)
	}
	news := map[string]NewsItem{}
	for _, n := range input.News {
		if n.AvailableAt.After(asOf) || n.PublishedAt.After(asOf) {
			continue
		}
		old, ok := news[n.ID]
		if !ok || n.AvailableAt.After(old.AvailableAt) {
			news[n.ID] = n
		}
	}
	for _, n := range news {
		out.News = append(out.News, n)
	}
	out.Instruments = append([]Instrument(nil), input.Instruments...)
	sort.Slice(out.Instruments, func(i, j int) bool { return out.Instruments[i].ID < out.Instruments[j].ID })
	sort.Slice(out.Actions, func(i, j int) bool { return out.Actions[i].ID < out.Actions[j].ID })
	sort.Slice(out.News, func(i, j int) bool { return out.News[i].ID < out.News[j].ID })
	sort.Slice(out.Bars, func(i, j int) bool {
		a, b := out.Bars[i], out.Bars[j]
		if a.InstrumentID != b.InstrumentID {
			return a.InstrumentID < b.InstrumentID
		}
		return a.SessionDate.Before(b.SessionDate)
	})
	return out, nil
}

// AdjustedBars changes research prices only for splits already effective at the cutoff.
// Execution continues to use original unadjusted bars.
func AdjustedBars(bars []Bar, actions []CorporateAction, asOf time.Time) ([]Bar, error) {
	out := append([]Bar(nil), bars...)
	for _, a := range actions {
		if a.Kind != "split" || a.EffectiveAt.After(asOf) || a.AvailableAt.After(asOf) {
			continue
		}
		if a.RatioNumerator <= 0 || a.RatioDenominator <= 0 {
			return nil, ErrCorporateActionIncomplete
		}
		// Providers bind splits to the session opening; date-only fixtures use
		// the same UTC date encoding as Bar.SessionDate. The effective day's
		// raw bar already uses the new shares in either representation.
		effectiveSession := dateUTC(a.EffectiveAt)
		for i := range out {
			b := &out[i]
			if b.InstrumentID == a.InstrumentID && b.SessionDate.Before(effectiveSession) {
				for _, p := range []*Price{&b.Open, &b.High, &b.Low, &b.Close} {
					v, e := scalePrice(*p, a.RatioDenominator, a.RatioNumerator)
					if e != nil {
						return nil, e
					}
					*p = v
				}
			}
		}
	}
	return out, nil
}
