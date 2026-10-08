package fixture_test

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"reflect"
	"testing"
	"time"
)

type deniedAccount struct{ ports.AccountStore }

func (deniedAccount) Get(context.Context, domain.Scope, string) (domain.Account, error) {
	return domain.Account{}, domain.ErrNotFound
}
func TestAnalysisAccountScope(t *testing.T) {
	f, _ := fixture.New()
	q := query.AnalysisQuery{Data: f, Accounts: deniedAccount{}, Mode: "fixture", Now: time.Now}
	_, e := q.Handle(context.Background(), dto.AnalysisRequest{AccountID: "other-owner", InstrumentID: "fixture-01"})
	if !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
}

func TestAnalysisReturnsRecentPriceSeries(t *testing.T) {
	f, _ := fixture.New()
	now := time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC)
	q := query.AnalysisQuery{Data: f, Mode: "fixture", Now: func() time.Time { return now }}
	v, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-01"})
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Prices) != 120 {
		t.Fatal("prices length", len(v.Prices))
	}
	previous := time.Time{}
	for _, p := range v.Prices {
		if p.Close <= 0 || !p.SessionDate.After(previous) {
			t.Fatal("prices not ascending or nonpositive", p)
		}
		previous = p.SessionDate
	}
	if last := v.Prices[len(v.Prices)-1]; !last.SessionDate.Equal(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("last price session", last.SessionDate)
	}
	unknown, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-99"})
	if !errors.Is(e, domain.ErrNotFound) || len(unknown.Prices) != 0 {
		t.Fatal(unknown.Prices, e)
	}
}

func TestAnalysisWithholdsUnprovenSplitFinancialUnits(t *testing.T) {
	f, _ := fixture.New()
	now := time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC)
	f.Snapshot.Actions = append(f.Snapshot.Actions, domain.CorporateAction{ID: "unit-proof", InstrumentID: "fixture-01", Kind: "split", EffectiveAt: time.Date(2026, 10, 6, 13, 30, 0, 0, time.UTC), AvailableAt: time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC), RatioNumerator: 2, RatioDenominator: 1})
	q := query.AnalysisQuery{Data: f, Mode: "fixture", Now: func() time.Time { return now }}
	v, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-01"})
	if e != nil || v.Metrics.Valuation.PE.Value != nil || v.Metrics.Valuation.MarketCap.Value != nil || v.Metrics.Valuation.PE.Reason != "split_reporting_basis_unverified" || v.Signal != nil || v.Metrics.Indicators.SMA200.Value == nil {
		t.Fatal(v.Metrics.Valuation, v.Signal, e)
	}
}
func TestNewsDoesNotChangeTradingSignal(t *testing.T) {
	f, _ := fixture.New()
	now := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	q := query.AnalysisQuery{Data: f, Mode: "fixture", Now: func() time.Time { return now }}
	before, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-01"})
	if e != nil || before.Signal == nil {
		t.Fatal(before, e)
	}
	f.Snapshot.News = append(f.Snapshot.News, domain.NewsItem{ID: "extra", Title: "Earnings surprise", InstrumentIDs: []string{"fixture-01"}, AvailableAt: now, PublishedAt: now})
	after, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-01"})
	if e != nil || !reflect.DeepEqual(before.Signal, after.Signal) || before.Risk.Level != after.Risk.Level || after.NewsStatus != "available" {
		t.Fatal(after, e)
	}
}

type unavailableData struct{ ports.ResearchData }

func (unavailableData) Read(context.Context, string, time.Time) (domain.Snapshot, error) {
	return domain.Snapshot{}, errors.New("temporary database failure")
}
func TestAnalysisServiceFailureRemainsFailure(t *testing.T) {
	q := query.DataStatusQuery{Data: unavailableData{}, Mode: "fixture", Now: time.Now}
	_, e := q.Handle(context.Background(), domain.Scope{}, "fixture")
	if e == nil {
		t.Fatal("failed data became empty")
	}
}
func TestAnalysisIndependentMissingAndPriceRisk(t *testing.T) {
	f, _ := fixture.New()
	now := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	for n := range f.Snapshot.Bars {
		b := &f.Snapshot.Bars[n]
		if b.InstrumentID == "fixture-27" && b.SessionDate.After(now.AddDate(0, 0, -35)) && b.SessionDate.Before(now) {
			if b.SessionDate.Day()%2 == 0 {
				b.Close *= 2
			}
		}
	}
	q := query.AnalysisQuery{Data: f, Mode: "fixture", Now: func() time.Time { return now }}
	v, e := q.Handle(context.Background(), dto.AnalysisRequest{InstrumentID: "fixture-27"})
	if e != nil || v.Signal != nil || v.Risk.Level != "high" || v.Recommendation.Potential != "unknown" {
		t.Fatal(v.Risk, v.Recommendation, e)
	}
}
