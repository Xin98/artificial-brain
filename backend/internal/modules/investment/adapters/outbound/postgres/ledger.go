package postgres

import (
	"context"
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func (s *LedgerStore) InsertAutomationEvent(ctx context.Context, scope domain.Scope, account, id, key, kind, reason string, projection any, effective, recorded time.Time) error {
	b, e := json.Marshal(projection)
	if e != nil {
		return e
	}
	_, e = database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, `insert into investment.automation_events(id,workspace_id,owner_user_id,account_id,event_key,kind,reason,projection,effective_at,recorded_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) on conflict(account_id,event_key) do nothing`, id, scope.WorkspaceID, scope.OwnerUserID, account, key, kind, reason, b, effective, recorded)
	return e
}

type LedgerStore struct{ pool *pgxpool.Pool }

func NewLedgerStore(p *pgxpool.Pool) *LedgerStore { return &LedgerStore{p} }
func (s *LedgerStore) InsertLedger(ctx context.Context, scope domain.Scope, account string, entries []domain.LedgerEntry) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	for _, l := range entries {
		if l.AccountID != account {
			return domain.ErrInvalidInput
		}
		_, e := exec.Exec(ctx, `insert into investment.ledger_entries(id,workspace_id,owner_user_id,account_id,event_key,kind,instrument_id,available_delta,reserved_delta,unsettled_delta,dividend_delta,quantity_delta,effective_at,recorded_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, l.ID, scope.WorkspaceID, scope.OwnerUserID, account, l.EventKey, l.Kind, l.InstrumentID, l.Delta.Available, l.Delta.Reserved, l.Delta.Unsettled, l.Delta.Dividends, l.QuantityDelta, l.EffectiveAt, l.RecordedAt)
		if e != nil {
			return e
		}
	}
	return nil
}
