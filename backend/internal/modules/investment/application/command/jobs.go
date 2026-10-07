package command

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type InvestmentJobHandler struct {
	UOW       ports.UnitOfWork
	Accounts  ports.WorkAccountStore
	Orders    ports.OrderStore
	Runs      ports.EvaluationStore
	SyncRuns  ports.SyncRunStore
	Source    ports.SourceSync
	Scheduler ports.JobScheduler
	Data      ports.ResearchData
	Evaluate  *EvaluateAccountHandler
	Backtest  *RunBacktestHandler
	Execute   ExecuteOrdersHandler
	Reconcile ReconcileAccountHandler
	Ledger    ports.AutomationEventStore
	Now       func() time.Time
	NewID     func() string
}

func (h *InvestmentJobHandler) HandleJob(ctx context.Context, args dto.InvestmentJobArgs, final bool) error {
	if args.JobType == "tick" {
		return h.tick(ctx)
	}
	scope := domain.Scope{WorkspaceID: args.WorkspaceID, OwnerUserID: args.OwnerUserID}
	if scope.WorkspaceID == "" || scope.OwnerUserID == "" {
		return domain.ErrInvalidInput
	}
	switch args.JobType {
	case "backtest":
		_, e := h.Backtest.Handle(ctx, dto.RunBacktestRequest{Scope: scope, RunID: args.RunID, FinalAttempt: final})
		return e
	case "sync":
		return h.sync(ctx, scope, args.RunID, final)
	case "evaluate":
		_, e := h.Evaluate.Handle(ctx, dto.EvaluateRequest{Scope: scope, AccountID: args.AccountID, RunID: args.RunID, Purpose: "automatic", SessionDate: args.SessionDate, FinalAttempt: final})
		if e != nil && final {
			mark := h.UOW.Run(ctx, func(ctx context.Context) error {
				_, err := h.Accounts.Lock(ctx, scope, args.AccountID)
				if err != nil {
					return err
				}
				v, err := h.Runs.GetEvaluation(ctx, scope, args.RunID)
				if err != nil {
					return err
				}
				if v.State != "queued" {
					return nil
				}
				v.State = "failed"
				v.Reason = "evaluation_failed"
				return h.Runs.UpdateEvaluation(ctx, scope, args.AccountID, v)
			})
			if mark != nil {
				return mark
			}
		}
		return e
	case "account":
		e := h.maintain(ctx, scope, args.AccountID)
		if e != nil && (final || errors.Is(e, domain.ErrDataNotConfigured) || errors.Is(e, domain.ErrDataStale) || errors.Is(e, domain.ErrCorporateActionIncomplete) || errors.Is(e, domain.ErrInvalidInput)) {
			mark := h.UOW.Run(ctx, func(ctx context.Context) error {
				_, err := h.Accounts.Lock(ctx, scope, args.AccountID)
				if err != nil {
					return err
				}
				key := "maintenance/" + h.Now().UTC().Truncate(15*time.Minute).Format(time.RFC3339)
				return h.Ledger.InsertAutomationEvent(ctx, scope, args.AccountID, h.NewID(), key, "blocked", "maintenance_data_unavailable", nil, h.Now(), h.Now())
			})
			if mark != nil {
				return mark
			}
			return nil
		}
		return e
	default:
		return domain.ErrInvalidInput
	}
}
func (h *InvestmentJobHandler) tick(ctx context.Context) error {
	refs, e := h.Accounts.WorkDue(ctx, h.Now())
	if e != nil {
		return e
	}
	for _, r := range refs {
		if e = h.UOW.Run(ctx, func(ctx context.Context) error {
			_, err := h.Scheduler.Enqueue(ctx, dto.InvestmentJobArgs{JobType: "account", WorkspaceID: r.Scope.WorkspaceID, OwnerUserID: r.Scope.OwnerUserID, AccountID: r.AccountID})
			return err
		}); e != nil {
			return e
		}
	}
	return nil
}
func (h *InvestmentJobHandler) sync(ctx context.Context, scope domain.Scope, id string, final bool) error {
	r, e := h.SyncRuns.GetSync(ctx, scope, id)
	if e != nil {
		return e
	}
	if r.View.Status == "completed" || r.View.Status == "failed" {
		return nil
	}
	r.View.Status = "running"
	r.View.UpdatedAt = h.Now()
	if e = h.UOW.Run(ctx, func(ctx context.Context) error { return h.SyncRuns.SaveSync(ctx, r) }); e != nil {
		return e
	}
	r.Request.Scope = scope
	e = h.Source.Sync(ctx, r.Request)
	r.View.UpdatedAt = h.Now()
	r.View.Status = "completed"
	r.View.Phase = "finished"
	if e != nil {
		r.View.Status = "running"
		r.View.ErrorCode = "source_temporarily_unavailable"
		r.View.Reason = "retry_pending"
		if final || errors.Is(e, domain.ErrDataNotConfigured) || errors.Is(e, domain.ErrInvalidInput) || errors.Is(e, domain.ErrVersionConflict) {
			r.View.Status = "failed"
			r.View.Reason = "source_sync_failed"
		}
	}
	if save := h.UOW.Run(ctx, func(ctx context.Context) error { return h.SyncRuns.SaveSync(ctx, r) }); save != nil {
		return save
	}
	if r.View.Status == "failed" {
		return nil
	}
	return e
}
func (h *InvestmentJobHandler) maintain(ctx context.Context, scope domain.Scope, id string) error {
	a, e := h.Accounts.Get(ctx, scope, id)
	if e != nil {
		return e
	}
	if a.Mode != "fixture" {
		pool, e := h.Reconcile.Catalog.GetUniverse(ctx, scope, a.UniverseVersionID)
		if e != nil {
			return e
		}
		// Incremental market refresh precedes reconciliation. Financial refresh is selected by the source service's daily cache.
		request := dto.SyncRequest{Mode: a.Mode, DatasetVersion: a.DatasetVersion, InstrumentIDs: pool.InstrumentIDs, From: h.Now().AddDate(0, 0, -7), To: h.Now()}
		run := dto.SyncRun{Scope: scope, Request: request, View: dto.RunView{ID: h.NewID(), Status: "running", Phase: "automatic_sources", CreatedAt: h.Now(), UpdatedAt: h.Now()}}
		if e = h.UOW.Run(ctx, func(ctx context.Context) error { return h.SyncRuns.InsertSync(ctx, run) }); e != nil {
			return e
		}
		sourceErr := h.Source.Sync(ctx, request)
		run.View.Status = "completed"
		run.View.UpdatedAt = h.Now()
		if sourceErr != nil {
			run.View.Status = "failed"
			run.View.ErrorCode = "source_unavailable"
			run.View.Reason = "automatic_sync_failed"
		}
		if e = h.UOW.Run(ctx, func(ctx context.Context) error { return h.SyncRuns.SaveSync(ctx, run) }); e != nil {
			return e
		}
		if sourceErr != nil {
			return sourceErr
		}
	}
	// First recover old fills; the executor blocks any split that has not yet been reconciled.
	executed, e := h.Execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: scope, AccountID: id})
	if e != nil {
		return e
	}
	reconciled, e := h.Reconcile.Handle(ctx, dto.ReconcileRequest{Scope: scope, AccountID: id})
	if e != nil {
		return e
	}
	// Corporate actions may have unblocked today's opening.
	if len(executed.Blocked) > 0 && reconciled.State == "completed" {
		executed, e = h.Execute.Handle(ctx, dto.ExecuteOrdersRequest{Scope: scope, AccountID: id})
		if e != nil {
			return e
		}
		reconciled, e = h.Reconcile.Handle(ctx, dto.ReconcileRequest{Scope: scope, AccountID: id})
		if e != nil {
			return e
		}
	}
	if reconciled.State != "completed" || len(executed.Blocked) > 0 {
		return h.UOW.Run(ctx, func(ctx context.Context) error {
			_, err := h.Accounts.Lock(ctx, scope, id)
			if err != nil {
				return err
			}
			key := "maintenance/" + h.Now().UTC().Truncate(15*time.Minute).Format(time.RFC3339)
			return h.Ledger.InsertAutomationEvent(ctx, scope, id, h.NewID(), key, "blocked", "corporate_action_review", reconciled, h.Now(), h.Now())
		})
	}
	snapshot, e := h.Data.Read(ctx, a.Mode, h.Now())
	if e != nil {
		return e
	}
	session, e := snapshot.Calendar.LatestCompleted(h.Now())
	if e != nil {
		return e
	}
	if h.Now().Before(session.CloseAt.Add(30 * time.Minute)) {
		return nil
	}
	e = h.UOW.Run(ctx, func(ctx context.Context) error {
		current, err := h.Accounts.Lock(ctx, scope, id)
		if err != nil {
			return err
		}
		if current.PendingConfig != nil && !h.Now().Before(current.PendingConfig.EffectiveAt) {
			p := current.PendingConfig
			current.StrategyVersionID = p.StrategyVersionID
			current.UniverseVersionID = p.UniverseVersionID
			current.Policy = p.Policy
			current.PendingConfig = nil
			if err = h.Accounts.Save(ctx, current, current.Version); err != nil {
				return err
			}
			current.Version++
		}
		book, err := h.Orders.LoadRiskBook(ctx, scope, id, session.Date)
		if err != nil {
			return err
		}
		for _, o := range book.Orders {
			if !o.Terminal() && !h.Now().Before(o.TargetOpenAt) {
				return nil
			}
		}
		nav, err := domain.ComputeNAV(current, book.Positions, snapshot, nil)
		if err != nil {
			return err
		}
		return h.Runs.SaveNAV(ctx, scope, id, domain.NAVPoint{SessionDate: session.Date, NAV: nav}, book.SessionTurnover, h.Now(), snapshot.QualityFlags)
	})
	if e != nil {
		return e
	}
	a, e = h.Accounts.Get(ctx, scope, id)
	if e != nil {
		return e
	}
	if !a.AutomationEnabled {
		return nil
	}
	_, e = h.Evaluate.Queue(ctx, dto.EvaluateRequest{Scope: scope, AccountID: id, Purpose: "automatic", SessionDate: session.Date})
	return e
}
