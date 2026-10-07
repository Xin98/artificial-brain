package query

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
)

type BacktestQuery struct{ Runs ports.BacktestStore }

func (h BacktestQuery) Handle(ctx context.Context, scope domain.Scope, id string) (dto.BacktestView, error) {
	r, e := h.Runs.GetBacktest(ctx, scope, id)
	if e != nil {
		return dto.BacktestView{}, e
	}
	return dto.ViewBacktest(r), nil
}

type SyncQuery struct{ Runs ports.SyncRunStore }

func (h SyncQuery) Handle(ctx context.Context, scope domain.Scope, id string) (dto.RunView, error) {
	r, e := h.Runs.GetSync(ctx, scope, id)
	return r.View, e
}

type EvaluationQuery struct {
	Runs     ports.RunStore
	Accounts ports.AccountStore
}

func (h EvaluationQuery) Handle(ctx context.Context, scope domain.Scope, account, id string) (dto.EvaluationReadView, error) {
	if _, e := h.Accounts.Get(ctx, scope, account); e != nil {
		return dto.EvaluationReadView{}, e
	}
	v, e := h.Runs.GetEvaluation(ctx, scope, id)
	if e != nil {
		return dto.EvaluationReadView{}, e
	}
	if v.AccountID != account {
		return dto.EvaluationReadView{}, domain.ErrNotFound
	}
	return dto.ViewEvaluation(v), nil
}
