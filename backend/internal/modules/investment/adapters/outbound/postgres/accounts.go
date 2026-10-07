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

type AccountStore struct{ pool *pgxpool.Pool }

func NewAccountStore(p *pgxpool.Pool) *AccountStore { return &AccountStore{p} }
func nullID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const accountColumns = `id::text,workspace_id::text,owner_user_id::text,name,mode,coalesce(strategy_version_id::text,''),coalesce(universe_version_id::text,''),initial_cash,available_cash,reserved_cash,unsettled_cash,dividend_receivable,version,automation_enabled,pause_reason,risk_policy,pending_config,created_at`

func scanAccount(row pgx.Row) (domain.Account, error) {
	a := domain.Account{}
	var policy, pending []byte
	e := row.Scan(&a.ID, &a.Scope.WorkspaceID, &a.Scope.OwnerUserID, &a.Name, &a.Mode, &a.StrategyVersionID, &a.UniverseVersionID, &a.InitialCash, &a.Balances.Available, &a.Balances.Reserved, &a.Balances.Unsettled, &a.Balances.Dividends, &a.Version, &a.AutomationEnabled, &a.PauseReason, &policy, &pending, &a.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, domain.ErrNotFound
	}
	if e != nil {
		return a, e
	}
	if e = json.Unmarshal(policy, &a.Policy); e != nil {
		return a, e
	}
	if len(pending) > 0 {
		e = json.Unmarshal(pending, &a.PendingConfig)
	}
	return a, e
}
func (s *AccountStore) Insert(ctx context.Context, a domain.Account) error {
	policy, e := json.Marshal(a.Policy)
	if e != nil {
		return e
	}
	pending, e := json.Marshal(a.PendingConfig)
	if e != nil {
		return e
	}
	_, e = database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.accounts(id,workspace_id,owner_user_id,name,mode,strategy_version_id,universe_version_id,initial_cash,available_cash,reserved_cash,unsettled_cash,dividend_receivable,version,automation_enabled,pause_reason,risk_policy,pending_config,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, a.ID, a.Scope.WorkspaceID, a.Scope.OwnerUserID, a.Name, a.Mode, nullID(a.StrategyVersionID), nullID(a.UniverseVersionID), a.InitialCash, a.Balances.Available, a.Balances.Reserved, a.Balances.Unsettled, a.Balances.Dividends, a.Version, a.AutomationEnabled, a.PauseReason, policy, pending, a.CreatedAt)
	return e
}
func (s *AccountStore) Get(ctx context.Context, scope domain.Scope, id string) (domain.Account, error) {
	return s.read(ctx, scope, id, false)
}
func (s *AccountStore) Lock(ctx context.Context, scope domain.Scope, id string) (domain.Account, error) {
	return s.read(ctx, scope, id, true)
}
func (s *AccountStore) read(ctx context.Context, scope domain.Scope, id string, lock bool) (domain.Account, error) {
	suffix := ""
	if lock {
		suffix = " for update"
	}
	return scanAccount(database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select `+accountColumns+` from investment.accounts where id=$1 and workspace_id=$2 and owner_user_id=$3`+suffix, id, scope.WorkspaceID, scope.OwnerUserID))
}
func (s *AccountStore) Save(ctx context.Context, a domain.Account, expected int) error {
	policy, e := json.Marshal(a.Policy)
	if e != nil {
		return e
	}
	pending, e := json.Marshal(a.PendingConfig)
	if e != nil {
		return e
	}
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `update investment.accounts set strategy_version_id=$5,universe_version_id=$6,available_cash=$7,reserved_cash=$8,unsettled_cash=$9,dividend_receivable=$10,version=$11,automation_enabled=$12,pause_reason=$13,risk_policy=$14,pending_config=$15 where id=$1 and workspace_id=$2 and owner_user_id=$3 and version=$4`, a.ID, a.Scope.WorkspaceID, a.Scope.OwnerUserID, expected, nullID(a.StrategyVersionID), nullID(a.UniverseVersionID), a.Balances.Available, a.Balances.Reserved, a.Balances.Unsettled, a.Balances.Dividends, expected+1, a.AutomationEnabled, a.PauseReason, policy, pending)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		if _, e = s.Get(ctx, a.Scope, a.ID); e != nil {
			return e
		}
		return domain.ErrVersionConflict
	}
	return nil
}
