package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"testing"
	"time"
)

func TestMarketBatchModeIsolationAndPreservation(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	data, e := fixture.New()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	s := NewSnapshotStore(p)
	tx := database.NewTxRunner(p)
	dataset := "fixture/synthetic/test-" + uuid()
	b := dto.MarketBatch{Mode: "fixture", Feed: "synthetic", Source: "fixture", Instruments: data.Snapshot.Instruments[:1], Bars: data.Snapshot.Bars[:2], Calendar: data.Snapshot.Calendar, AsOf: now, QualityFlags: []string{"demonstration_data"}}
	if e = tx.Run(ctx, func(ctx context.Context) error { return s.AppendMarket(ctx, dataset, b) }); e != nil {
		t.Fatal(e)
	}
	old, e := s.Find(ctx, dataset, now)
	if e != nil || len(old.Bars) != 2 {
		t.Fatal(old, e)
	}
	b.AsOf = now.Add(time.Minute)
	b.Bars = data.Snapshot.Bars[2:3]
	if e = tx.Run(ctx, func(ctx context.Context) error { return s.AppendMarket(ctx, dataset, b) }); e != nil {
		t.Fatal(e)
	}
	latest, e := s.Find(ctx, dataset, b.AsOf)
	if e != nil || len(latest.Bars) != 3 {
		t.Fatal(latest, e)
	}
	unchanged, e := s.Get(ctx, dataset, old.ID)
	if e != nil || len(unchanged.Bars) != 2 {
		t.Fatal(unchanged, e)
	}
	b.Mode = "alpaca_sec"
	b.Feed = "sip"
	b.AsOf = b.AsOf.Add(time.Minute)
	if e = tx.Run(ctx, func(ctx context.Context) error { return s.AppendMarket(ctx, dataset, b) }); e == nil {
		t.Fatal("mode/feed overwritten")
	}
}
