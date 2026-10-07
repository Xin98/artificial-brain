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

func New() (*Adapter, error) {
	s := domain.Snapshot{ID: "fixture-v1", DatasetVersion: "fixture-v1", Mode: "fixture", Feed: "synthetic", QualityFlags: []string{"demonstration_data", "fixed_universe_survivorship_bias"}}
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
		s.Bars = append(s.Bars, b)
		if b.AvailableAt.After(s.AsOf) {
			s.AsOf = b.AvailableAt
		}
	}
	return &Adapter{Snapshot: s}, nil
}
