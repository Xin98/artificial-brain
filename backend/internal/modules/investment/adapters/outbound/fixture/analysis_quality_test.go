package fixture_test

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/query"
	"testing"
	"time"
)

func TestAnalysisUnknownIndustryAndStalePriceAreUnknownRisk(t *testing.T) {
	for _, kind := range []string{"industry", "price"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := fixture.New()
			now := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
			if kind == "industry" {
				f.Snapshot.Instruments[0].SIC = ""
			} else {
				for n := range f.Snapshot.Bars {
					b := &f.Snapshot.Bars[n]
					if b.InstrumentID == "fixture-01" && b.SessionDate.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)) {
						b.AvailableAt = now.AddDate(0, 0, 1)
					}
				}
			}
			q := query.AnalysisQuery{Data: f, Mode: "fixture", Now: func() time.Time { return now }}
			v, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-01"})
			if e != nil || v.Risk.Level != "unknown" {
				t.Fatal(v.Risk, e)
			}
		})
	}
}
