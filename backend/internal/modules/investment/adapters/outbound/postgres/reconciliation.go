package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"time"
)

const ledgerColumns = "id::text,account_id::text,event_key,kind,instrument_id,available_delta,reserved_delta,unsettled_delta,dividend_delta,quantity_delta,effective_at,recorded_at"

func scanLedger(row pgx.Row) (domain.LedgerEntry, error) {
	var l domain.LedgerEntry
	e := row.Scan(&l.ID, &l.AccountID, &l.EventKey, &l.Kind, &l.InstrumentID, &l.Delta.Available, &l.Delta.Reserved, &l.Delta.Unsettled, &l.Delta.Dividends, &l.QuantityDelta, &l.EffectiveAt, &l.RecordedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = domain.ErrNotFound
	}
	return l, e
}
func (s *LedgerStore) ClaimEvent(ctx context.Context, scope domain.Scope, account, key string) (bool, error) {
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.automation_events(id,workspace_id,owner_user_id,account_id,event_key,kind,reason,projection,effective_at,recorded_at) select gen_random_uuid(),workspace_id,owner_user_id,id,$4,'reconcile_event','','{}'::jsonb,now(),now() from investment.accounts where workspace_id=$1 and owner_user_id=$2 and id=$3 on conflict(account_id,event_key) do nothing`, scope.WorkspaceID, scope.OwnerUserID, account, key)
	if e != nil {
		return false, e
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	if _, e = NewAccountStore(s.pool).Get(ctx, scope, account); e != nil {
		return false, e
	}
	return false, nil
}
func (s *LedgerStore) Unsettled(ctx context.Context, scope domain.Scope, account string) ([]domain.LedgerEntry, error) {
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, "select "+ledgerColumns+` from investment.ledger_entries l where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and kind='sell_fill' and not exists(select 1 from investment.ledger_entries paid where paid.account_id=l.account_id and paid.event_key='settlement/'||l.event_key) order by effective_at,id`, scope.WorkspaceID, scope.OwnerUserID, account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.LedgerEntry{}
	for rows.Next() {
		l, e := scanLedger(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *LedgerStore) EventEntry(ctx context.Context, scope domain.Scope, account, key string) (domain.LedgerEntry, error) {
	return scanLedger(database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, "select "+ledgerColumns+" from investment.ledger_entries where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and event_key=$4", scope.WorkspaceID, scope.OwnerUserID, account, key))
}
func (s *LedgerStore) actionRecord(ctx context.Context, scope domain.Scope, account, id string) (dto.ActionRecord, error) {
	var b []byte
	var out dto.ActionRecord
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select projection from investment.automation_events where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and event_key=$4`, scope.WorkspaceID, scope.OwnerUserID, account, "action/"+id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, domain.ErrNotFound
	}
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(b, &out)
	if e != nil {
		return out, e
	}
	if out.Action.ID != id {
		return out, domain.ErrCorporateActionIncomplete
	}
	return out, nil
}
func (s *LedgerStore) RecordedAction(ctx context.Context, scope domain.Scope, account, id string) (domain.CorporateAction, error) {
	r, e := s.actionRecord(ctx, scope, account, id)
	return r.Action, e
}
func (s *LedgerStore) SaveActionEligibility(ctx context.Context, scope domain.Scope, account string, action domain.CorporateAction, eligible []domain.Position) error {
	b, e := json.Marshal(dto.ActionRecord{Action: action, Eligibility: eligible})
	if e != nil {
		return e
	}
	tag, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `update investment.automation_events set projection=$5,effective_at=$6 where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and event_key=$4 and (projection='{}'::jsonb or projection->'eligibility'='null'::jsonb or projection=$5::jsonb)`, scope.WorkspaceID, scope.OwnerUserID, account, "action/"+action.ID, b, action.EffectiveAt)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrVersionConflict
	}
	return nil
}
func (s *LedgerStore) ActionEligibility(ctx context.Context, scope domain.Scope, account, id string) ([]domain.Position, error) {
	record, e := s.actionRecord(ctx, scope, account, id)
	if e != nil {
		return nil, e
	}
	if record.Eligibility != nil {
		return record.Eligibility, nil
	}
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select l.instrument_id,coalesce(p.industry,''),sum(l.quantity_delta) from investment.ledger_entries l left join investment.positions p on p.account_id=l.account_id and p.instrument_id=l.instrument_id where l.workspace_id=$1 and l.owner_user_id=$2 and l.account_id=$3 and l.instrument_id=$4 and (l.effective_at<$5 or (l.effective_at=$5 and l.kind='split')) group by l.instrument_id,p.industry having sum(l.quantity_delta)>0 order by l.instrument_id`, scope.WorkspaceID, scope.OwnerUserID, account, record.Action.InstrumentID, record.Action.EffectiveAt)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Position{}
	for rows.Next() {
		var p domain.Position
		if e = rows.Scan(&p.InstrumentID, &p.Industry, &p.Quantity); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *LedgerStore) InventoryChangedSince(ctx context.Context, scope domain.Scope, account, instrument string, at time.Time) (bool, error) {
	var changed bool
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select exists(select 1 from investment.fills where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and instrument_id=$4 and effective_at >= $5) or exists(select 1 from investment.ledger_entries where workspace_id=$1 and owner_user_id=$2 and account_id=$3 and instrument_id=$4 and kind='split' and effective_at>$5)`, scope.WorkspaceID, scope.OwnerUserID, account, instrument, at).Scan(&changed)
	return changed, e
}
