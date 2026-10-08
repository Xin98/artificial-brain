package fixture

import (
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strconv"
	"strings"
	"time"
)

//go:embed testdata/*
var files embed.FS

type Adapter struct{ Snapshot domain.Snapshot }

func (a *Adapter) Read(ctx context.Context, mode string, asOf time.Time) (domain.Snapshot, error) {
	if e := ctx.Err(); e != nil {
		return domain.Snapshot{}, e
	}
	if mode != "fixture" {
		return domain.Snapshot{}, domain.ErrDataNotConfigured
	}
	return domain.SelectSnapshot(a.Snapshot, asOf)
}

func (a *Adapter) Instruments(ctx context.Context, ids []string) ([]domain.Instrument, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var out []domain.Instrument
	for _, i := range a.Snapshot.Instruments {
		if len(ids) == 0 || wanted[i.ID] {
			out = append(out, i)
		}
	}
	return out, nil
}
func (a *Adapter) Bars(ctx context.Context, ids []string, from, to time.Time) ([]domain.Bar, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var out []domain.Bar
	for _, b := range a.Snapshot.Bars {
		if wanted[b.InstrumentID] && !b.SessionDate.Before(from) && !b.SessionDate.After(to) {
			out = append(out, b)
		}
	}
	return out, nil
}
func (a *Adapter) Calendar(ctx context.Context, from, to time.Time) (domain.Calendar, error) {
	if e := ctx.Err(); e != nil {
		return domain.Calendar{}, e
	}
	return a.Snapshot.Calendar, nil
}
func (a *Adapter) Actions(ctx context.Context, ids []string, from, to time.Time) ([]domain.CorporateAction, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var out []domain.CorporateAction
	for _, v := range a.Snapshot.Actions {
		if wanted[v.InstrumentID] && !v.EffectiveAt.Before(from) && !v.EffectiveAt.After(to) {
			out = append(out, v)
		}
	}
	return out, nil
}

func New() (*Adapter, error) {
	s := domain.Snapshot{ID: "fixture-v2", DatasetVersion: "fixture/synthetic/v2", Mode: "fixture", Feed: "synthetic", QualityFlags: []string{"demonstration_data", "fixed_universe_survivorship_bias"}}
	for _, f := range []struct {
		name   string
		target any
	}{{"calendar.json", &s.Calendar}, {"instruments.json", &s.Instruments}, {"facts.json", &s.Facts}, {"actions.json", &s.Actions}, {"news.json", &s.News}} {
		b, e := files.ReadFile("testdata/" + f.name)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, f.target); e != nil {
			return nil, fmt.Errorf("%s: %w", f.name, e)
		}
	}
	data, e := files.ReadFile("testdata/bars.csv")
	if e != nil {
		return nil, e
	}
	rows, e := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if e != nil {
		return nil, e
	}
	for _, row := range rows[1:] {
		if len(row) != 8 {
			return nil, fmt.Errorf("invalid fixture bar")
		}
		b := domain.Bar{InstrumentID: row[0], Provenance: domain.Provenance{Source: "fixture", SourceRecordID: row[0] + "/" + row[1]}}
		b.SessionDate, e = time.Parse(time.RFC3339, row[1])
		if e != nil {
			return nil, e
		}
		b.AvailableAt, e = time.Parse(time.RFC3339, row[2])
		if e != nil {
			return nil, e
		}
		b.IngestedAt = b.AvailableAt
		for i, p := range []*domain.Price{&b.Open, &b.High, &b.Low, &b.Close} {
			v, er := strconv.ParseInt(row[3+i], 10, 64)
			if er != nil || v <= 0 {
				return nil, fmt.Errorf("invalid price")
			}
			*p = domain.Price(v)
		}
		v, er := strconv.ParseInt(row[7], 10, 64)
		if er != nil || v < 0 {
			return nil, fmt.Errorf("invalid volume")
		}
		b.Volume = domain.Quantity(v)
		b.NoOpeningTrade = v == 0
		s.Bars = append(s.Bars, b)
		if b.AvailableAt.After(s.AsOf) {
			s.AsOf = b.AvailableAt
		}
	}
	return &Adapter{Snapshot: s}, nil
}
