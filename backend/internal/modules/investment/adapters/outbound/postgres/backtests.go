package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
)

func (s *RunStore) InsertBacktest(ctx context.Context, scope domain.Scope, r dto.BacktestRecord) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.backtest_runs(id,workspace_id,owner_user_id,state,dataset_version,strategy_version_id,universe_version_id,request,result,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$8,$9)`, r.View.ID, scope.WorkspaceID, scope.OwnerUserID, r.View.Status, r.DatasetVersion, r.Request.StrategyVersionID, r.Request.UniverseVersionID, b, r.View.CreatedAt)
	return e
}
func (s *RunStore) readBacktest(ctx context.Context, scope domain.Scope, id string, lock bool) (dto.BacktestRecord, error) {
	var b []byte
	var r dto.BacktestRecord
	suffix := ""
	if lock {
		suffix = " for update"
	}
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select result from investment.backtest_runs where workspace_id=$1 and owner_user_id=$2 and id=$3`+suffix, scope.WorkspaceID, scope.OwnerUserID, id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, domain.ErrNotFound
	}
	if e != nil {
		return r, e
	}
	e = json.Unmarshal(b, &r)
	return r, e
}
func (s *RunStore) LockBacktest(ctx context.Context, scope domain.Scope, id string) (dto.BacktestRecord, error) {
	return s.readBacktest(ctx, scope, id, true)
}
func (s *RunStore) GetBacktest(ctx context.Context, scope domain.Scope, id string) (dto.BacktestRecord, error) {
	return s.readBacktest(ctx, scope, id, false)
}
func (s *RunStore) SaveBacktest(ctx context.Context, scope domain.Scope, r dto.BacktestRecord, expected int) error {
	r.Version = expected + 1
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `update investment.backtest_runs set state=$5,result=$6,error_code=$7 where workspace_id=$1 and owner_user_id=$2 and id=$3 and (result->>'Version')::integer=$4 and state in ('queued','running')`, scope.WorkspaceID, scope.OwnerUserID, r.View.ID, expected, r.View.Status, b, r.View.ErrorCode)
	if e == nil && tag.RowsAffected() != 1 {
		return domain.ErrVersionConflict
	}
	return e
}
