package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"testing"
	"time"
)

func TestSECFinancialPersistenceKeepsAccessions(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := NewSnapshotStore(p)
	tx := database.NewTxRunner(p)
	f, _ := fixture.New()
	dataset := "fixture/synthetic/sec-test-" + uuid()
	old := f.Snapshot
	old.ID = "initial-" + uuid()
	old.DatasetVersion = dataset
	old.AsOf = time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	old.Facts = nil
	if e := tx.Run(ctx, func(ctx context.Context) error { return s.Insert(ctx, old) }); e != nil {
		t.Fatal(e)
	}
	facts := []domain.FinancialFact{{InstrumentID: "fixture-01", Accession: "original", Concept: "NetIncome", Value: "100", Unit: "USD", Currency: "USD", PeriodStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), AvailableAt: time.Date(2026, 3, 1, 15, 0, 0, 0, time.UTC), Provenance: domain.Provenance{Source: "sec-test", SourceRecordID: "original/net", IngestedAt: time.Now()}}}
	facts = append(facts, facts[0])
	facts[1].Accession = "amended"
	facts[1].Value = "80"
	facts[1].SourceRecordID = "amended/net"
	facts[1].AvailableAt = time.Date(2026, 4, 1, 15, 0, 0, 0, time.UTC)
	if e := tx.Run(ctx, func(ctx context.Context) error { return s.AppendFinancials(ctx, dataset, facts) }); e != nil {
		t.Fatal(e)
	}
	latest, e := s.Find(ctx, dataset, time.Now().Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	march, e := domain.SelectSnapshot(latest, old.AsOf)
	if e != nil || len(march.Facts) != 1 || march.Facts[0].Value != "100" {
		t.Fatal(march.Facts, e)
	}
	var count int
	if e = p.QueryRow(ctx, "select count(*) from investment.financial_facts where dataset_version=$1", dataset).Scan(&count); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	unchanged, e := s.Get(ctx, dataset, old.ID)
	if e != nil || len(unchanged.Facts) != 0 {
		t.Fatal(e)
	}
	facts[0].Value = "900"
	e = tx.Run(ctx, func(ctx context.Context) error { return s.AppendFinancials(ctx, dataset, facts[:1]) })
	if !errors.Is(e, domain.ErrVersionConflict) {
		t.Fatal(e)
	}
}
