package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type ResearchReader struct {
	Store                *SnapshotStore
	Mode, DatasetVersion string
}

func (r ResearchReader) Read(ctx context.Context, mode string, at time.Time) (domain.Snapshot, error) {
	if mode != r.Mode {
		return domain.Snapshot{}, domain.ErrDataNotConfigured
	}
	s, e := r.Store.Find(ctx, r.DatasetVersion, at)
	if e != nil {
		return s, e
	}
	return domain.SelectSnapshot(s, at)
}
