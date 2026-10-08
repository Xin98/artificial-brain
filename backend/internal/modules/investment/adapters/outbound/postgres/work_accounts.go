package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"time"
)

// WorkDue is an internal scheduler inventory. Public reads always require Scope.
func (s *AccountStore) WorkDue(ctx context.Context, _ time.Time) ([]dto.AccountRef, error) {
	rows, e := database.ExecutorFromContextOr(ctx, s.pool).Query(ctx, `select a.workspace_id::text,a.owner_user_id::text,a.id::text from investment.accounts a where automation_enabled or pending_config is not null and pending_config<>'null'::jsonb or unsettled_cash>0 or dividend_receivable>0 or exists(select 1 from investment.orders o where o.account_id=a.id and o.state in ('pending','awaiting_bar')) or exists(select 1 from investment.positions p where p.account_id=a.id and p.quantity>0) order by a.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []dto.AccountRef{}
	for rows.Next() {
		var r dto.AccountRef
		if e = rows.Scan(&r.Scope.WorkspaceID, &r.Scope.OwnerUserID, &r.AccountID); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
