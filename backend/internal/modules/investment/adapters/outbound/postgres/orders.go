package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type OrderStore struct{ pool *pgxpool.Pool }

func (s *OrderStore) SessionCapacityLimit(ctx context.Context, scope domain.Scope, account, instrument string, session time.Time) (domain.Quantity, error) {
	var value *int64
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select min(capacity) from investment.orders where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and instrument_id=$4 and target_open_at >= $5::date and target_open_at < $5::date+interval '1 day'`, scope.WorkspaceID, scope.OwnerUserID, account, instrument, session.UTC().Format("2006-01-02")).Scan(&value)
	if e != nil {
		return 0, e
	}
	if value == nil {
		return 0, domain.ErrNotFound
	}
	return domain.Quantity(*value), nil
}
func NewOrderStore(p *pgxpool.Pool) *OrderStore { return &OrderStore{p} }

const orderColumns = "id::text,account_id::text,instrument_id,side,state,reason,origin,quantity,reserved_quantity,reserved_cash,capacity,target_open_at,expires_at,created_at,version"

func scanOrder(row pgx.Row) (domain.Order, error) {
	var o domain.Order
	e := row.Scan(&o.ID, &o.AccountID, &o.InstrumentID, &o.Side, &o.State, &o.Reason, &o.Origin, &o.Quantity, &o.ReservedQuantity, &o.ReservedCash, &o.Capacity, &o.TargetOpenAt, &o.ExpiresAt, &o.CreatedAt, &o.Version)
	if errors.Is(e, pgx.ErrNoRows) {
		e = domain.ErrNotFound
	}
	return o, e
}
func (s *OrderStore) InsertOrder(ctx context.Context, scope domain.Scope, account string, o domain.Order) error {
	if o.AccountID != account {
		return domain.ErrInvalidInput
	}
	_, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.orders(id,workspace_id,owner_user_id,account_id,instrument_id,side,state,reason,origin,quantity,reserved_quantity,reserved_cash,capacity,target_open_at,expires_at,created_at,version) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, o.ID, scope.WorkspaceID, scope.OwnerUserID, account, o.InstrumentID, o.Side, o.State, o.Reason, o.Origin, o.Quantity, o.ReservedQuantity, o.ReservedCash, o.Capacity, o.TargetOpenAt, o.ExpiresAt, o.CreatedAt, o.Version)
	return e
}
func (s *OrderStore) GetOrder(ctx context.Context, scope domain.Scope, account, id string) (domain.Order, error) {
	return s.read(ctx, scope, account, id, false)
}
func (s *OrderStore) LockOrder(ctx context.Context, scope domain.Scope, account, id string) (domain.Order, error) {
	return s.read(ctx, scope, account, id, true)
}
func (s *OrderStore) read(ctx context.Context, scope domain.Scope, account, id string, lock bool) (domain.Order, error) {
	suffix := ""
	if lock {
		suffix = " for update"
	}
	return scanOrder(database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, "select "+orderColumns+" from investment.orders where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and id=$4"+suffix, scope.WorkspaceID, scope.OwnerUserID, account, id))
}
func (s *OrderStore) ListPending(ctx context.Context, scope domain.Scope, account string) ([]domain.Order, error) {
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, "select "+orderColumns+" from investment.orders where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and state in ('pending','awaiting_bar') order by instrument_id,id for update", scope.WorkspaceID, scope.OwnerUserID, account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *OrderStore) SaveOrder(ctx context.Context, scope domain.Scope, account string, o domain.Order, expected int) error {
	if o.AccountID != account {
		return domain.ErrInvalidInput
	}
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `update investment.orders set state=$6,reason=$7,reserved_cash=$8,reserved_quantity=$9,version=version+1 where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and id=$4 and version=$5 and state in ('pending','awaiting_bar')`, scope.WorkspaceID, scope.OwnerUserID, account, o.ID, expected, o.State, o.Reason, o.ReservedCash, o.ReservedQuantity)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		if _, e = s.GetOrder(ctx, scope, account, o.ID); e != nil {
			return e
		}
		return domain.ErrVersionConflict
	}
	return nil
}
func (s *OrderStore) InsertFill(ctx context.Context, scope domain.Scope, account string, f domain.Fill) error {
	if f.AccountID != account {
		return domain.ErrInvalidInput
	}
	_, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.fills(id,workspace_id,owner_user_id,account_id,order_id,instrument_id,quantity,price,gross,fee,effective_at,recorded_at,settles_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, f.ID, scope.WorkspaceID, scope.OwnerUserID, account, f.OrderID, f.InstrumentID, f.Quantity, f.Price, f.Gross, f.Fee, f.EffectiveAt, f.RecordedAt, nullTime(f.SettlesAt))
	return e
}
func (s *OrderStore) GetFill(ctx context.Context, scope domain.Scope, account, order string) (domain.Fill, error) {
	var f domain.Fill
	var settlement *time.Time
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select f.id::text,f.account_id::text,f.order_id::text,f.instrument_id,o.side,o.reason,f.quantity,f.price,f.gross,f.fee,f.effective_at,f.recorded_at,f.settles_at from investment.fills f join investment.orders o on o.id=f.order_id where f.workspace_id=$1 and f.owner_user_id=$2 and f.account_id=$3 and f.order_id=$4`, scope.WorkspaceID, scope.OwnerUserID, account, order).Scan(&f.ID, &f.AccountID, &f.OrderID, &f.InstrumentID, &f.Side, &f.Reason, &f.Quantity, &f.Price, &f.Gross, &f.Fee, &f.EffectiveAt, &f.RecordedAt, &settlement)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, domain.ErrNotFound
	}
	if settlement != nil {
		f.SettlesAt = *settlement
	}
	return f, e
}
func (s *OrderStore) LoadPositions(ctx context.Context, scope domain.Scope, account string) ([]domain.Position, error) {
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select instrument_id,industry,quantity,reserved_quantity,cost from investment.positions where workspace_id=$1 and owner_user_id=$2 and account_id=$3 order by instrument_id for update`, scope.WorkspaceID, scope.OwnerUserID, account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Position{}
	for rows.Next() {
		var p domain.Position
		if e = rows.Scan(&p.InstrumentID, &p.Industry, &p.Quantity, &p.ReservedQuantity, &p.CostBasis); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *OrderStore) SavePositions(ctx context.Context, scope domain.Scope, account string, positions []domain.Position) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	for _, p := range positions {
		_, e := exec.Exec(ctx, `insert into investment.positions(workspace_id,owner_user_id,account_id,instrument_id,industry,quantity,reserved_quantity,cost) values($1,$2,$3,$4,$5,$6,$7,$8) on conflict(account_id,instrument_id) do update set industry=excluded.industry,quantity=excluded.quantity,reserved_quantity=excluded.reserved_quantity,cost=excluded.cost where investment.positions.workspace_id=$1 and investment.positions.owner_user_id=$2`, scope.WorkspaceID, scope.OwnerUserID, account, p.InstrumentID, p.Industry, p.Quantity, p.ReservedQuantity, p.CostBasis)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *OrderStore) SessionCapacityUsed(ctx context.Context, scope domain.Scope, account, instrument string, session time.Time) (domain.Quantity, error) {
	var q domain.Quantity
	date := session.UTC().Format("2006-01-02")
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select coalesce(sum(quantity),0) from investment.fills where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and instrument_id=$4 and effective_at >= $5::date and effective_at < $5::date+interval '1 day'`, scope.WorkspaceID, scope.OwnerUserID, account, instrument, date).Scan(&q)
	return q, e
}
func (s *OrderStore) LoadRiskBook(ctx context.Context, scope domain.Scope, account string, session time.Time) (dto.RiskBook, error) {
	out := dto.RiskBook{}
	a, e := NewAccountStore(s.pool).Get(ctx, scope, account)
	if e != nil {
		return out, e
	}
	out.Positions, e = s.LoadPositions(ctx, scope, account)
	if e != nil {
		return out, e
	}
	out.Orders, e = s.ListPending(ctx, scope, account)
	if e != nil {
		return out, e
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	e = exec.QueryRow(ctx, `select greatest(coalesce(max(nav),0),$4::bigint) from investment.nav_snapshots where workspace_id=$1 and owner_user_id=$2 and account_id=$3`, scope.WorkspaceID, scope.OwnerUserID, account, a.InitialCash).Scan(&out.PeakNAV)
	if e != nil {
		return out, e
	}
	e = exec.QueryRow(ctx, `select coalesce(sum(gross),0) from investment.fills where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and effective_at >= $4::date and effective_at < $4::date+interval '1 day'`, scope.WorkspaceID, scope.OwnerUserID, account, session.UTC().Format("2006-01-02")).Scan(&out.SessionTurnover)
	return out, e
}
