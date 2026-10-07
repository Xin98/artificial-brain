package command

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type CreateBacktestHandler struct {
	Mutations application.MutationExecutor
	Runs      ports.BacktestStore
	Catalog   ports.CatalogStore
	Snapshots ports.SnapshotStore
	History   ports.HistoryData
	Scheduler ports.JobScheduler
	Mode      string
	Now       func() time.Time
	NewID     func() string
}

func (h CreateBacktestHandler) Handle(ctx context.Context, r dto.CreateBacktestRequest) (dto.RunView, error) {
	if r.InitialCash == "" {
		r.InitialCash = "100000.00"
	}
	cash, e := domain.ParseMoney(r.InitialCash)
	if e != nil || cash <= 0 || cash > 100000000000 || r.From.IsZero() || r.To.Before(r.From) || r.To.After(h.Now()) || r.To.Sub(r.From) > 5*366*24*time.Hour {
		return dto.RunView{}, domain.ErrInvalidInput
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.RunView, error) {
		u, e := h.Catalog.GetUniverse(ctx, r.Scope, r.UniverseVersionID)
		if e != nil {
			return dto.RunView{}, e
		}
		if u.Mode != h.Mode {
			return dto.RunView{}, domain.ErrInvalidInput
		}
		if _, e = h.Catalog.GetStrategy(ctx, r.Scope, r.StrategyVersionID); e != nil {
			return dto.RunView{}, e
		}
		source, e := h.History.History(ctx, h.Mode, h.Now())
		if e != nil {
			return dto.RunView{}, e
		}
		source.ID = "backtest/" + h.NewID()
		source.AsOf = h.Now()
		if e = h.Snapshots.Insert(ctx, source); e != nil {
			return dto.RunView{}, fmt.Errorf("freeze backtest input: %w", e)
		}
		v := dto.RunView{ID: h.NewID(), Status: "queued", Phase: "replay", CreatedAt: h.Now(), UpdatedAt: h.Now()}
		record := dto.BacktestRecord{Request: r, DatasetVersion: source.DatasetVersion, Mode: h.Mode, SnapshotIDs: []string{source.ID}, View: v, Version: 1}
		if e = h.Runs.InsertBacktest(ctx, r.Scope, record); e != nil {
			return v, fmt.Errorf("persist backtest: %w", e)
		}
		if _, e = h.Scheduler.Enqueue(ctx, dto.InvestmentJobArgs{JobType: "backtest", WorkspaceID: r.Scope.WorkspaceID, OwnerUserID: r.Scope.OwnerUserID, RunID: v.ID}); e != nil {
			return v, fmt.Errorf("schedule backtest: %w", e)
		}
		return v, nil
	})
}
