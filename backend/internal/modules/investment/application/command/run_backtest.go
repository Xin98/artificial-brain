package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type RunBacktestHandler struct {
	UOW       ports.UnitOfWork
	Runs      ports.BacktestStore
	Catalog   ports.CatalogStore
	Snapshots ports.SnapshotStore
	Now       func() time.Time
}

func (h RunBacktestHandler) Handle(ctx context.Context, r dto.RunBacktestRequest) (dto.RunView, error) {
	view, e := h.run(ctx, r)
	if e != nil && r.FinalAttempt {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		mark := h.UOW.Run(cleanup, func(ctx context.Context) error {
			current, err := h.Runs.LockBacktest(ctx, r.Scope, r.RunID)
			if err != nil {
				return err
			}
			if current.View.Status == "completed" || current.View.Status == "failed" {
				view = current.View
				return nil
			}
			current.View.Status = "failed"
			current.View.ErrorCode = "backtest_worker_failed"
			current.View.Reason = "retries_exhausted"
			current.View.UpdatedAt = h.Now()
			current.Result = nil
			if err = h.Runs.SaveBacktest(ctx, r.Scope, current, current.Version); err != nil {
				return err
			}
			view = current.View
			return nil
		})
		return view, mark
	}
	return view, e
}
func (h RunBacktestHandler) run(ctx context.Context, r dto.RunBacktestRequest) (dto.RunView, error) {
	record, e := h.Runs.GetBacktest(ctx, r.Scope, r.RunID)
	if e != nil {
		return dto.RunView{}, e
	}
	if record.View.Status == "completed" || record.View.Status == "failed" {
		return record.View, nil
	}
	e = h.UOW.Run(ctx, func(ctx context.Context) error {
		current, err := h.Runs.LockBacktest(ctx, r.Scope, r.RunID)
		if err != nil {
			return err
		}
		if current.View.Status == "queued" {
			current.View.Status = "running"
			current.View.UpdatedAt = h.Now()
			if err = h.Runs.SaveBacktest(ctx, r.Scope, current, current.Version); err != nil {
				return err
			}
			current.Version++
		}
		record = current
		return nil
	})
	if e != nil {
		return record.View, e
	}
	if record.View.Status == "completed" || record.View.Status == "failed" {
		return record.View, nil
	}
	u, e := h.Catalog.GetUniverse(ctx, r.Scope, record.Request.UniverseVersionID)
	if e != nil {
		return record.View, e
	}
	strategy, e := h.Catalog.GetStrategy(ctx, r.Scope, record.Request.StrategyVersionID)
	if e != nil {
		return record.View, e
	}
	sources := []domain.Snapshot{}
	for _, id := range record.SnapshotIDs {
		s, err := h.Snapshots.Get(ctx, record.DatasetVersion, id)
		if err != nil {
			return record.View, err
		}
		sources = append(sources, s)
	}
	if len(sources) == 0 {
		return record.View, domain.ErrDataStale
	}
	cash, e := domain.ParseMoney(record.Request.InitialCash)
	if e != nil {
		return record.View, e
	}
	benchmark := ""
	for _, i := range sources[0].Instruments {
		if i.Ticker == "SPY" {
			benchmark = i.ID
		}
	}
	result, replayErr := domain.ReplayWithContext(ctx, domain.BacktestInput{Snapshots: sources, Calendar: sources[0].Calendar, Universe: u, Strategy: strategy, InitialCash: cash, From: record.Request.From, To: record.Request.To, BenchmarkID: benchmark})
	if ctx.Err() != nil {
		return record.View, ctx.Err()
	}
	var view dto.RunView
	e = h.UOW.Run(ctx, func(ctx context.Context) error {
		current, err := h.Runs.LockBacktest(ctx, r.Scope, r.RunID)
		if err != nil {
			return err
		}
		if current.View.Status == "completed" || current.View.Status == "failed" {
			view = current.View
			return nil
		}
		current.View.UpdatedAt = h.Now()
		current.View.Status = "completed"
		current.View.Phase = "finished"
		current.Result = &result
		if replayErr != nil {
			current.View.Status = "failed"
			current.View.ErrorCode = "backtest_data_incomplete"
			current.View.Reason = replayErr.Error()
			current.Result = nil
		}
		if err = h.Runs.SaveBacktest(ctx, r.Scope, current, current.Version); err != nil {
			return err
		}
		view = current.View
		return nil
	})
	return view, e
}
