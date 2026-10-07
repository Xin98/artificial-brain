package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type RunStore struct{ pool *pgxpool.Pool }

func (s *RunStore) IssuedOn(ctx context.Context, scope domain.Scope, account string, session time.Time) (bool, error) {
	var out bool
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select exists(select 1 from investment.evaluation_runs where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and session_date=$4::date and issued_orders)`, scope.WorkspaceID, scope.OwnerUserID, account, session.UTC().Format("2006-01-02")).Scan(&out)
	return out, e
}

func NewRunStore(p *pgxpool.Pool) *RunStore { return &RunStore{p} }
func (s *RunStore) SaveEvaluation(ctx context.Context, scope domain.Scope, account string, v domain.Evaluation) error {
	v.AccountID = account
	if v.SessionDate.IsZero() {
		v.SessionDate = v.AsOf
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, e = exec.Exec(ctx, `insert into investment.evaluation_runs(id,workspace_id,owner_user_id,account_id,session_date,strategy_version_id,universe_version_id,dataset_version,snapshot_id,mode,state,issued_orders,as_of,projection) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, v.ID, scope.WorkspaceID, scope.OwnerUserID, account, v.SessionDate.UTC().Format("2006-01-02"), v.StrategyVersionID, v.UniverseVersionID, v.DatasetVersion, v.SnapshotID, v.Mode, v.State, v.IssuedOrders, v.AsOf, b)
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
func (s *RunStore) ClaimEvaluation(ctx context.Context, scope domain.Scope, account string, session time.Time, strategy, mode string) (dto.EvaluationClaim, error) {
	var b []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select projection from investment.evaluation_runs where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and session_date=$4::date and strategy_version_id=$5 and mode=$6 for update`, scope.WorkspaceID, scope.OwnerUserID, account, session.UTC().Format("2006-01-02"), strategy, mode).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return dto.EvaluationClaim{}, nil
	}
	if e != nil {
		return dto.EvaluationClaim{}, e
	}
	var v domain.Evaluation
	e = json.Unmarshal(b, &v)
	return dto.EvaluationClaim{Found: true, Evaluation: v}, e
}
func (s *RunStore) UpdateEvaluation(ctx context.Context, scope domain.Scope, account string, v domain.Evaluation) error {
	if v.AccountID != account {
		return domain.ErrInvalidInput
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	tag, e := exec.Exec(ctx, `update investment.evaluation_runs set state=$5,issued_orders=$6,projection=$7 where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and id=$4 and state='queued' and snapshot_id=$8 and strategy_version_id=$9 and universe_version_id=$10`, scope.WorkspaceID, scope.OwnerUserID, account, v.ID, v.State, v.IssuedOrders, b, v.SnapshotID, v.StrategyVersionID, v.UniverseVersionID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrVersionConflict
	}
	if v.State == "queued" {
		return nil
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
		rec, e := json.Marshal(sig.Recommendation)
		if e != nil {
			return e
		}
		_, e = exec.Exec(ctx, `insert into investment.recommendations(workspace_id,owner_user_id,run_id,instrument_id,projection) values($1,$2,$3,$4,$5)`, scope.WorkspaceID, scope.OwnerUserID, v.ID, sig.InstrumentID, rec)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *RunStore) SaveNAV(ctx context.Context, scope domain.Scope, account string, point domain.NAVPoint, turnover domain.Money, available time.Time, flags []string) error {
	if flags == nil {
		flags = []string{}
	}
	b, e := json.Marshal(flags)
	if e != nil {
		return e
	}
	_, e = database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.nav_snapshots(workspace_id,owner_user_id,account_id,session_date,nav,gross_turnover,available_at,quality_flags) values($1,$2,$3,$4::date,$5,$6,$7,$8) on conflict(account_id,session_date) do nothing`, scope.WorkspaceID, scope.OwnerUserID, account, point.SessionDate.UTC().Format("2006-01-02"), point.NAV, turnover, available, b)
	return e
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
