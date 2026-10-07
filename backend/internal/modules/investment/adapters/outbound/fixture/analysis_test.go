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
