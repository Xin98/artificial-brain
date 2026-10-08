package postgres

import (
	"context"
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RequestStore struct{ pool *pgxpool.Pool }

func NewRequestStore(p *pgxpool.Pool) *RequestStore { return &RequestStore{p} }
func (s *RequestStore) Claim(ctx context.Context, scope domain.Scope, route, key, hash string) (dto.RequestRecord, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	tag, e := exec.Exec(ctx, `insert into investment.mutation_requests(workspace_id,owner_user_id,route,key,request_hash) values($1,$2,$3,$4,$5) on conflict do nothing`, scope.WorkspaceID, scope.OwnerUserID, route, key, hash)
	if e != nil {
		return dto.RequestRecord{}, e
	}
	r := dto.RequestRecord{Claimed: tag.RowsAffected() > 0}
	e = exec.QueryRow(ctx, `select request_hash,response from investment.mutation_requests where workspace_id=$1 and owner_user_id=$2 and route=$3 and key=$4 for update`, scope.WorkspaceID, scope.OwnerUserID, route, key).Scan(&r.Hash, &r.Response)
	return r, e
}
func (s *RequestStore) Complete(ctx context.Context, scope domain.Scope, route, key string, response json.RawMessage) error {
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `update investment.mutation_requests set response=$5 where workspace_id=$1 and owner_user_id=$2 and route=$3 and key=$4 and response is null`, scope.WorkspaceID, scope.OwnerUserID, route, key, []byte(response))
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrVersionConflict
	}
	return nil
}
