package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"testing"
	"time"
)

func TestNewsSnapshotSeparatesUnavailableCoverage(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := NewSnapshotStore(p)
	tx := database.NewTxRunner(p)
	f, _ := fixture.New()
	at := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	snapshot, _ := f.Read(ctx, "fixture", at)
	snapshot.DatasetVersion = "fixture/synthetic/news-test-" + uuid()
	snapshot.ID = "news-base-" + uuid()
	if e := tx.Run(ctx, func(ctx context.Context) error { return s.Insert(ctx, snapshot) }); e != nil {
		t.Fatal(e)
	}
	at = at.Add(time.Minute)
	if e := tx.Run(ctx, func(ctx context.Context) error {
		return s.AppendNews(ctx, snapshot.DatasetVersion, nil, "unavailable", at)
	}); e != nil {
		t.Fatal(e)
	}
	latest, e := s.Find(ctx, snapshot.DatasetVersion, at)
	if e != nil {
		t.Fatal(e)
	}
	has := false
	for _, flag := range latest.QualityFlags {
		if flag == "news_unavailable" {
			has = true
		}
	}
	if !has {
		t.Fatal(latest.QualityFlags)
	}
	before, e := s.Get(ctx, snapshot.DatasetVersion, snapshot.ID)
	if e != nil || len(before.QualityFlags) != len(snapshot.QualityFlags) {
		t.Fatal(e)
	}
	at = at.Add(time.Minute)
	news := []domain.NewsItem{{ID: "a", Title: "Earnings", URL: "https://example.com/a", InstrumentIDs: []string{"fixture-01"}, PublishedAt: at, AvailableAt: at, Provenance: domain.Provenance{Source: "fixture", SourceRecordID: "a", IngestedAt: at}}}
	if e := tx.Run(ctx, func(ctx context.Context) error {
		return s.AppendNews(ctx, snapshot.DatasetVersion, news, "available", at)
	}); e != nil {
		t.Fatal(e)
	}
	latest, e = s.Find(ctx, snapshot.DatasetVersion, at)
	if e != nil {
		t.Fatal(e)
	}
	for _, flag := range latest.QualityFlags {
		if flag == "news_unavailable" {
			t.Fatal(latest.QualityFlags)
		}
	}
}
