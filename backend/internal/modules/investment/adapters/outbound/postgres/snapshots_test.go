package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"testing"
	"time"
)

func TestInvestmentSnapshotImmutable(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := NewSnapshotStore(p)
	v := domain.Snapshot{ID: "snapshot-test", DatasetVersion: "snapshot-test-v1", Mode: "fixture", Feed: "synthetic", AsOf: time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC), QualityFlags: []string{"demonstration_data"}}
	_, _ = p.Exec(ctx, "delete from investment.data_snapshots where dataset_version=$1", v.DatasetVersion)
	_, _ = p.Exec(ctx, "delete from investment.datasets where version=$1", v.DatasetVersion)
	defer p.Exec(ctx, "delete from investment.datasets where version=$1", v.DatasetVersion)
	defer p.Exec(ctx, "delete from investment.data_snapshots where dataset_version=$1", v.DatasetVersion)
	tx := database.NewTxRunner(p)
	if e := tx.Run(ctx, func(ctx context.Context) error { return s.Insert(ctx, v) }); e != nil {
		t.Fatal(e)
	}
	if e := s.Insert(ctx, v); e != nil {
		t.Fatal(e)
	}
	v.QualityFlags = []string{"changed"}
	if e := s.Insert(ctx, v); !errors.Is(e, domain.ErrVersionConflict) {
		t.Fatal(e)
	}
	old, e := s.Get(ctx, v.DatasetVersion, v.ID)
	if e != nil || old.QualityFlags[0] != "demonstration_data" {
		t.Fatal(old, e)
	}
	if _, e = s.Find(ctx, v.DatasetVersion, v.AsOf.Add(-time.Hour)); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
}
