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

type RunStore struct{ pool *pgxpool.Pool }

func NewRunStore(p *pgxpool.Pool) *RunStore { return &RunStore{p} }
func (s *RunStore) SaveEvaluation(ctx context.Context, scope domain.Scope, account string, v domain.Evaluation) error {
	v.AccountID = account
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, e = exec.Exec(ctx, `insert into investment.evaluation_runs(id,workspace_id,owner_user_id,account_id,session_date,strategy_version_id,universe_version_id,dataset_version,snapshot_id,mode,state,issued_orders,as_of,projection) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, v.ID, scope.WorkspaceID, scope.OwnerUserID, account, v.AsOf.Format("2006-01-02"), v.StrategyVersionID, v.UniverseVersionID, v.DatasetVersion, v.SnapshotID, v.Mode, v.State, v.IssuedOrders, v.AsOf, b)
	if e != nil {
		return e
	}
	for _, sig := range v.Signals {
		evidence, e := json.Marshal(sig.FactorEvidence)
		if e != nil {
			return e
		}
		_, e = exec.Exec(ctx, `insert into investment.signals(workspace_id,owner_user_id,run_id,instrument_id,score,rank,evidence) values($1,$2,$3,$4,$5,$6,$7)`, scope.WorkspaceID, scope.OwnerUserID, v.ID, sig.InstrumentID, sig.Score, sig.Rank, evidence)
		if e != nil {
			return e
		}
		recommendation, e := json.Marshal(sig.Recommendation)
		if e != nil {
			return e
		}
		_, e = exec.Exec(ctx, `insert into investment.recommendations(workspace_id,owner_user_id,run_id,instrument_id,projection) values($1,$2,$3,$4,$5)`, scope.WorkspaceID, scope.OwnerUserID, v.ID, sig.InstrumentID, recommendation)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *RunStore) GetEvaluation(ctx context.Context, scope domain.Scope, id string) (domain.Evaluation, error) {
	var b []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select projection from investment.evaluation_runs where id=$1 and workspace_id=$2 and owner_user_id=$3`, id, scope.WorkspaceID, scope.OwnerUserID).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Evaluation{}, domain.ErrNotFound
	}
	if e != nil {
		return domain.Evaluation{}, e
	}
	var v domain.Evaluation
	e = json.Unmarshal(b, &v)
	return v, e
}
