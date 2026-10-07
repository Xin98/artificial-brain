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
	for _, f := range facts {
		out.Facts = append(out.Facts, f)
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
	sort.Slice(out.Facts, func(i, j int) bool {
		a, b := out.Facts[i], out.Facts[j]
		return fmt.Sprint(a.InstrumentID, a.Concept, a.PeriodEnd, a.PeriodStart, a.Unit, a.Currency) < fmt.Sprint(b.InstrumentID, b.Concept, b.PeriodEnd, b.PeriodStart, b.Unit, b.Currency)
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
		for i := range out {
			b := &out[i]
			if b.InstrumentID == a.InstrumentID && b.SessionDate.Before(a.EffectiveAt) {
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
