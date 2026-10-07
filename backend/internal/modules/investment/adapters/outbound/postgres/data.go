package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type SnapshotStore struct{ pool *pgxpool.Pool }

func NewSnapshotStore(p *pgxpool.Pool) *SnapshotStore { return &SnapshotStore{p} }
func (s *SnapshotStore) Insert(ctx context.Context, v domain.Snapshot) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	coverage, e := json.Marshal(v.QualityFlags)
	if e != nil {
		return e
	}
	_, e = exec.Exec(ctx, `insert into investment.datasets(version,mode,feed,source,created_at,coverage) values($1,$2,$3,$4,$5,$6) on conflict do nothing`, v.DatasetVersion, v.Mode, v.Feed, v.Mode, v.AsOf, coverage)
	if e != nil {
		return e
	}
	var mode, feed string
	if e = exec.QueryRow(ctx, `select mode,feed from investment.datasets where version=$1`, v.DatasetVersion).Scan(&mode, &feed); e != nil {
		return e
	}
	if mode != v.Mode || feed != v.Feed {
		return domain.ErrVersionConflict
	}
	tag, e := exec.Exec(ctx, `insert into investment.data_snapshots(id,dataset_version,as_of,projection) values($1,$2,$3,$4) on conflict do nothing`, v.ID, v.DatasetVersion, v.AsOf, b)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		var equal bool
		if e = exec.QueryRow(ctx, `select projection=$3::jsonb from investment.data_snapshots where dataset_version=$1 and id=$2`, v.DatasetVersion, v.ID, b).Scan(&equal); e != nil {
			return e
		}
		if !equal {
			return domain.ErrVersionConflict
		}
	}
	return nil
}
func (s *SnapshotStore) Get(ctx context.Context, dataset, id string) (domain.Snapshot, error) {
	var b []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select projection from investment.data_snapshots where dataset_version=$1 and id=$2`, dataset, id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Snapshot{}, domain.ErrNotFound
	}
	if e != nil {
		return domain.Snapshot{}, e
	}
	var out domain.Snapshot
	e = json.Unmarshal(b, &out)
	return out, e
}
func (s *SnapshotStore) Find(ctx context.Context, dataset string, asOf time.Time) (domain.Snapshot, error) {
	var id string
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select id from investment.data_snapshots where dataset_version=$1 and as_of<=$2 order by as_of desc,id limit 1`, dataset, asOf).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Snapshot{}, domain.ErrNotFound
	}
	if e != nil {
		return domain.Snapshot{}, e
	}
	return s.Get(ctx, dataset, id)
}
