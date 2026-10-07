package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CatalogStore struct{ pool *pgxpool.Pool }

func NewCatalogStore(p *pgxpool.Pool) *CatalogStore { return &CatalogStore{p} }
func (s *CatalogStore) InsertUniverse(ctx context.Context, scope domain.Scope, v domain.UniverseVersion) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, e := exec.Exec(ctx, `insert into investment.universe_versions(id,universe_id,workspace_id,owner_user_id,name,mode,effective_at,created_at,historical_membership_known) values($1,$2,$3,$4,$5,$6,$7,$8,$9)`, v.ID, v.UniverseID, scope.WorkspaceID, scope.OwnerUserID, v.Name, v.Mode, v.EffectiveAt, v.CreatedAt, v.HistoricalMembershipKnown)
	if e != nil {
		return e
	}
	for _, id := range v.InstrumentIDs {
		if _, e = exec.Exec(ctx, `insert into investment.universe_members(workspace_id,owner_user_id,version_id,instrument_id) values($1,$2,$3,$4)`, scope.WorkspaceID, scope.OwnerUserID, v.ID, id); e != nil {
			return e
		}
	}
	return nil
}
func (s *CatalogStore) GetUniverse(ctx context.Context, scope domain.Scope, id string) (domain.UniverseVersion, error) {
	v := domain.UniverseVersion{Scope: scope, InstrumentIDs: []string{}}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	e := exec.QueryRow(ctx, `select id::text,universe_id::text,name,mode,effective_at,created_at,historical_membership_known from investment.universe_versions where id=$1 and workspace_id=$2 and owner_user_id=$3`, id, scope.WorkspaceID, scope.OwnerUserID).Scan(&v.ID, &v.UniverseID, &v.Name, &v.Mode, &v.EffectiveAt, &v.CreatedAt, &v.HistoricalMembershipKnown)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, domain.ErrNotFound
	}
	if e != nil {
		return v, e
	}
	rows, e := exec.Query(ctx, `select instrument_id from investment.universe_members where version_id=$1 and workspace_id=$2 and owner_user_id=$3 order by instrument_id`, id, scope.WorkspaceID, scope.OwnerUserID)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return v, e
		}
		v.InstrumentIDs = append(v.InstrumentIDs, id)
	}
	return v, rows.Err()
}
func (s *CatalogStore) InsertStrategy(ctx context.Context, scope domain.Scope, v domain.StrategyVersion) error {
	b, e := json.Marshal(v.Parameters)
	if e != nil {
		return e
	}
	_, e = database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.strategy_versions(id,strategy_id,workspace_id,owner_user_id,parameters,created_at) values($1,$2,$3,$4,$5,$6)`, v.ID, v.StrategyID, scope.WorkspaceID, scope.OwnerUserID, b, v.CreatedAt)
	return e
}
func (s *CatalogStore) GetStrategy(ctx context.Context, scope domain.Scope, id string) (domain.StrategyVersion, error) {
	v := domain.StrategyVersion{Scope: scope}
	var b []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select id::text,strategy_id,parameters,created_at from investment.strategy_versions where id=$1 and workspace_id=$2 and owner_user_id=$3`, id, scope.WorkspaceID, scope.OwnerUserID).Scan(&v.ID, &v.StrategyID, &b, &v.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, domain.ErrNotFound
	}
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(b, &v.Parameters)
	return v, e
}
