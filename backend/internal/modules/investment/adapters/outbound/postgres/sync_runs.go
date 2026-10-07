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

func (s *RunStore) InsertSync(ctx context.Context, r dto.SyncRun) error {
	request, e := json.Marshal(r.Request)
	if e != nil {
		return e
	}
	result, e := json.Marshal(r.View)
	if e != nil {
		return e
	}
	_, e = database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.data_sync_runs(id,workspace_id,owner_user_id,dataset_version,mode,state,request,result,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.View.ID, r.Scope.WorkspaceID, r.Scope.OwnerUserID, r.Request.DatasetVersion, r.Request.Mode, r.View.Status, request, result, r.View.CreatedAt)
	return e
}
func (s *RunStore) GetSync(ctx context.Context, scope domain.Scope, id string) (dto.SyncRun, error) {
	r := dto.SyncRun{Scope: scope}
	var request, result []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select request,result from investment.data_sync_runs where workspace_id=$1 and owner_user_id=$2 and id=$3`, scope.WorkspaceID, scope.OwnerUserID, id).Scan(&request, &result)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, domain.ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(request, &r.Request); e != nil {
		return r, e
	}
	r.Request.Scope = scope
	e = json.Unmarshal(result, &r.View)
	return r, e
}
func (s *RunStore) SaveSync(ctx context.Context, r dto.SyncRun) error {
	b, e := json.Marshal(r.View)
	if e != nil {
		return e
	}
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `update investment.data_sync_runs set state=$4,result=$5,error_code=$6,completed_at=case when $4 in ('completed','failed') then $7::timestamptz else null end where workspace_id=$1 and owner_user_id=$2 and id=$3 and state in ('queued','running')`, r.Scope.WorkspaceID, r.Scope.OwnerUserID, r.View.ID, r.View.Status, b, r.View.ErrorCode, r.View.UpdatedAt)
	if e == nil && tag.RowsAffected() == 0 {
		return domain.ErrVersionConflict
	}
	return e
}
