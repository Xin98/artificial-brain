package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type ReadStore struct{ pool *pgxpool.Pool }

func NewReadStore(p *pgxpool.Pool) *ReadStore { return &ReadStore{p} }
func (s *ReadStore) ListRows(ctx context.Context, r dto.ListRequest, after dto.PageCursor) ([]dto.ListRow, error) {
	out := []dto.ListRow{}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	if r.Resource == "positions" {
		rows, e := exec.Query(ctx, `select instrument_id,industry,quantity,reserved_quantity,cost from investment.positions where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and instrument_id>$4 and quantity>0 order by instrument_id limit $5`, r.Scope.WorkspaceID, r.Scope.OwnerUserID, r.ParentID, after.ID, r.Limit)
		if e != nil {
			return out, e
		}
		defer rows.Close()
		for rows.Next() {
			var p domain.Position
			if e = rows.Scan(&p.InstrumentID, &p.Industry, &p.Quantity, &p.ReservedQuantity, &p.CostBasis); e != nil {
				return out, e
			}
			out = append(out, dto.ListRow{ID: p.InstrumentID, Value: p})
		}
		return out, rows.Err()
	}
	table := ""
	stamp := "created_at"
	parent := ""
	switch r.Resource {
	case "accounts":
		table = "accounts"
	case "universes":
		table = "universe_versions"
	case "strategies":
		table = "strategy_versions"
	case "orders":
		table = "orders"
		parent = " and account_id=$6::uuid"
	case "ledger":
		table = "ledger_entries"
		stamp = "recorded_at"
		parent = " and account_id=$6::uuid"
	case "evaluations":
		table = "evaluation_runs"
		stamp = "as_of"
		parent = " and account_id=$6::uuid"
	case "automation-events":
		table = "automation_events"
		stamp = "recorded_at"
		parent = " and account_id=$6::uuid"
	case "backtests":
		table = "backtest_runs"
	case "data-syncs":
		table = "data_sync_runs"
	default:
		return out, domain.ErrInvalidInput
	}
	var at any
	if !after.At.IsZero() {
		at = after.At
	}
	args := []any{r.Scope.WorkspaceID, r.Scope.OwnerUserID, at, after.ID, r.Limit}
	if parent != "" {
		args = append(args, r.ParentID)
	}
	sql := fmt.Sprintf("select id::text,%s from investment.%s where workspace_id=$1 and owner_user_id=$2 and ($3::timestamptz is null or (%s,id::text)<($3::timestamptz,$4::text))%s order by %s desc,id desc limit $5", stamp, table, stamp, parent, stamp)
	rows, e := exec.Query(ctx, sql, args...)
	if e != nil {
		return out, e
	}
	// Consume the bounded key page before fetching projections; one connection also works inside an ambient transaction.
	for rows.Next() {
		var row dto.ListRow
		if e = rows.Scan(&row.ID, &row.At); e != nil {
			rows.Close()
			return out, e
		}
		out = append(out, row)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for n, row := range out {
		var value any
		switch r.Resource {
		case "accounts":
			v, err := NewAccountStore(s.pool).Get(ctx, r.Scope, row.ID)
			if err != nil {
				return out, err
			}
			value = dto.ViewAccount(v)
		case "universes":
			v, err := NewCatalogStore(s.pool).GetUniverse(ctx, r.Scope, row.ID)
			if err != nil {
				return out, err
			}
			value = dto.VersionView{ID: v.ID, ParentID: v.UniverseID, Name: v.Name, Mode: v.Mode, CreatedAt: v.CreatedAt, EffectiveAt: v.EffectiveAt, InstrumentIDs: v.InstrumentIDs}
		case "strategies":
			v, err := NewCatalogStore(s.pool).GetStrategy(ctx, r.Scope, row.ID)
			if err != nil {
				return out, err
			}
			value = dto.VersionView{ID: v.ID, ParentID: v.StrategyID, Name: "可解释多因子 v1", CreatedAt: v.CreatedAt, Parameters: &v.Parameters}
		case "orders":
			store := NewOrderStore(s.pool)
			v, err := store.GetOrder(ctx, r.Scope, r.ParentID, row.ID)
			if err != nil {
				return out, err
			}
			view := dto.ViewOrder(v)
			fill, err := store.GetFill(ctx, r.Scope, r.ParentID, row.ID)
			if err != nil && err != domain.ErrNotFound {
				return out, err
			}
			if err == nil {
				f := dto.ViewFill(fill)
				view.Fill = &f
			}
			value = view
		case "evaluations":
			v, err := NewRunStore(s.pool).GetEvaluation(ctx, r.Scope, row.ID)
			if err != nil {
				return out, err
			}
			value = dto.ViewEvaluation(v)
		case "backtests":
			v, err := NewRunStore(s.pool).GetBacktest(ctx, r.Scope, row.ID)
			if err != nil {
				return out, err
			}
			value = dto.ViewBacktest(v)
		case "data-syncs":
			v, err := NewRunStore(s.pool).GetSync(ctx, r.Scope, row.ID)
			if err != nil {
				return out, err
			}
			value = v.View
		case "ledger":
			var v domain.LedgerEntry
			e = exec.QueryRow(ctx, `select id::text,account_id::text,event_key,kind,instrument_id,available_delta,reserved_delta,unsettled_delta,dividend_delta,quantity_delta,effective_at,recorded_at from investment.ledger_entries where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and id=$4`, r.Scope.WorkspaceID, r.Scope.OwnerUserID, r.ParentID, row.ID).Scan(&v.ID, &v.AccountID, &v.EventKey, &v.Kind, &v.InstrumentID, &v.Delta.Available, &v.Delta.Reserved, &v.Delta.Unsettled, &v.Delta.Dividends, &v.QuantityDelta, &v.EffectiveAt, &v.RecordedAt)
			if e != nil {
				return out, e
			}
			value = v
		case "automation-events":
			var v dto.AutomationEventView
			e = exec.QueryRow(ctx, `select id::text,kind,reason,effective_at,recorded_at from investment.automation_events where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and id=$4`, r.Scope.WorkspaceID, r.Scope.OwnerUserID, r.ParentID, row.ID).Scan(&v.ID, &v.Kind, &v.Reason, &v.EffectiveAt, &v.RecordedAt)
			if e != nil {
				return out, e
			}
			value = v
		}
		out[n].Value = value
	}
	return out, nil
}
func (s *ReadStore) LatestNAV(ctx context.Context, scope domain.Scope, account string) (*dto.RecordedNAV, error) {
	var v dto.RecordedNAV
	var b []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select session_date,nav,gross_turnover,available_at,quality_flags from investment.nav_snapshots where workspace_id=$1 and owner_user_id=$2 and account_id=$3 order by session_date desc limit 1`, scope.WorkspaceID, scope.OwnerUserID, account).Scan(&v.Point.SessionDate, &v.Point.NAV, &v.Turnover, &v.AvailableAt, &b)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	e = json.Unmarshal(b, &v.QualityFlags)
	return &v, e
}
func (s *ReadStore) NAVHistory(ctx context.Context, scope domain.Scope, account string) ([]dto.RecordedNAV, error) {
	out := []dto.RecordedNAV{}
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select session_date,nav,gross_turnover,available_at,quality_flags from investment.nav_snapshots where workspace_id=$1 and owner_user_id=$2 and account_id=$3 order by session_date`, scope.WorkspaceID, scope.OwnerUserID, account)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v dto.RecordedNAV
		var b []byte
		if e = rows.Scan(&v.Point.SessionDate, &v.Point.NAV, &v.Turnover, &v.AvailableAt, &b); e != nil {
			return out, e
		}
		if e = json.Unmarshal(b, &v.QualityFlags); e != nil {
			return out, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *ReadStore) PerformanceFills(ctx context.Context, scope domain.Scope, account string) ([]domain.Fill, error) {
	out := []domain.Fill{}
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select f.id::text,f.account_id::text,f.order_id::text,f.instrument_id,o.side,o.reason,f.quantity,f.price,f.gross,f.fee,f.effective_at,f.recorded_at,f.settles_at from investment.fills f join investment.orders o on o.id=f.order_id and o.workspace_id=f.workspace_id and o.owner_user_id=f.owner_user_id where f.workspace_id=$1 and f.owner_user_id=$2 and f.account_id=$3 order by f.effective_at,f.instrument_id,f.id`, scope.WorkspaceID, scope.OwnerUserID, account)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var f domain.Fill
		var settles *time.Time
		if e = rows.Scan(&f.ID, &f.AccountID, &f.OrderID, &f.InstrumentID, &f.Side, &f.Reason, &f.Quantity, &f.Price, &f.Gross, &f.Fee, &f.EffectiveAt, &f.RecordedAt, &settles); e != nil {
			return out, e
		}
		if settles != nil {
			f.SettlesAt = *settles
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
