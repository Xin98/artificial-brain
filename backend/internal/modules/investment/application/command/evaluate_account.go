package command

import (
	"context"
	"errors"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type EvaluateAccountHandler struct {
	Mutations    application.MutationExecutor
	UOW          ports.UnitOfWork
	Accounts     ports.AccountStore
	Catalog      ports.CatalogStore
	Orders       ports.OrderStore
	Ledger       ports.AutomationEventStore
	Runs         ports.EvaluationStore
	Snapshots    ports.SnapshotStore
	Data         ports.ResearchData
	Reservations ReservationWriter
	Scheduler    ports.JobScheduler
	Now          func() time.Time
	NewID        func() string
}

func (h EvaluateAccountHandler) Prepare(ctx context.Context, r dto.EvaluateRequest) (domain.Evaluation, error) {
	var out domain.Evaluation
	e := h.UOW.Run(ctx, func(ctx context.Context) error { var e error; out, e = h.prepare(ctx, r); return e })
	return out, e
}
func (h EvaluateAccountHandler) prepare(ctx context.Context, r dto.EvaluateRequest) (domain.Evaluation, error) {
	if r.Purpose == "" {
		r.Purpose = "research"
	}
	if r.Purpose != "research" && r.Purpose != "automatic" {
		return domain.Evaluation{}, domain.ErrInvalidInput
	}
	account, e := h.Accounts.Lock(ctx, r.Scope, r.AccountID)
	if e != nil {
		return domain.Evaluation{}, e
	}
	if r.RunID != "" {
		v, e := h.Runs.GetEvaluation(ctx, r.Scope, r.RunID)
		if e != nil {
			return v, e
		}
		if v.AccountID != account.ID {
			return v, domain.ErrNotFound
		}
		return v, nil
	}
	now := h.Now()
	if account.PendingConfig != nil && !now.Before(account.PendingConfig.EffectiveAt) {
		cfg := account.PendingConfig
		account.StrategyVersionID = cfg.StrategyVersionID
		account.UniverseVersionID = cfg.UniverseVersionID
		account.Policy = cfg.Policy
		account.PendingConfig = nil
		if e = h.Accounts.Save(ctx, account, account.Version); e != nil {
			return domain.Evaluation{}, e
		}
		account.Version++
	}
	data, e := h.Data.Read(ctx, account.Mode, now)
	if e != nil {
		return domain.Evaluation{}, e
	}
	if e = domain.ValidateAccountDataset(account, data); e != nil {
		return domain.Evaluation{}, e
	}
	var session domain.Session
	if r.SessionDate.IsZero() {
		session, e = data.Calendar.LatestCompleted(now)
	} else {
		session, e = data.Calendar.Session(r.SessionDate)
	}
	if e != nil {
		return domain.Evaluation{}, e
	}
	if now.Before(session.CloseAt) || (r.Purpose == "automatic" && now.Before(session.CloseAt.Add(30*time.Minute))) {
		return domain.Evaluation{}, domain.ErrDataStale
	}
	claim, e := h.Runs.ClaimEvaluation(ctx, r.Scope, account.ID, session.Date, account.StrategyVersionID, account.Mode, r.Purpose)
	if e != nil {
		return domain.Evaluation{}, e
	}
	if claim.Found {
		return claim.Evaluation, nil
	}
	id := h.NewID()
	data.ID = "decision/" + id
	data.AsOf = now
	if e = h.Snapshots.Insert(ctx, data); e != nil {
		return domain.Evaluation{}, e
	}
	out := domain.Evaluation{ID: id, AccountID: account.ID, SnapshotID: data.ID, DatasetVersion: data.DatasetVersion, Mode: account.Mode, SessionDate: session.Date, AsOf: now, StrategyVersionID: account.StrategyVersionID, UniverseVersionID: account.UniverseVersionID, FrozenPolicy: account.Policy, Purpose: r.Purpose, State: "queued", Signals: []domain.Signal{}, Excluded: []domain.Exclusion{}, OrderIDs: []string{}, QualityFlags: data.QualityFlags}
	if e = h.Runs.SaveEvaluation(ctx, r.Scope, account.ID, out); e != nil {
		return out, e
	}
	return out, nil
}
func evaluationRunView(v domain.Evaluation) dto.RunView {
	return dto.RunView{ID: v.ID, Status: v.State, Phase: "evaluation", Reason: v.Reason, CreatedAt: v.AsOf, UpdatedAt: v.AsOf}
}
func (h EvaluateAccountHandler) enqueue(ctx context.Context, r dto.EvaluateRequest) (dto.RunView, error) {
	v, e := h.prepare(ctx, r)
	if e != nil {
		return dto.RunView{}, e
	}
	if v.State != "queued" || v.JobID != 0 {
		return evaluationRunView(v), nil
	}
	id, e := h.Scheduler.Enqueue(ctx, dto.InvestmentJobArgs{JobType: "evaluate", WorkspaceID: r.Scope.WorkspaceID, OwnerUserID: r.Scope.OwnerUserID, AccountID: v.AccountID, RunID: v.ID, SessionDate: v.SessionDate})
	if e != nil {
		return dto.RunView{}, fmt.Errorf("enqueue evaluation: %w", e)
	}
	v.JobID = id
	if e = h.Runs.UpdateEvaluation(ctx, r.Scope, v.AccountID, v); e != nil {
		return dto.RunView{}, fmt.Errorf("record evaluation job: %w", e)
	}
	return evaluationRunView(v), nil
}
func (h EvaluateAccountHandler) Queue(ctx context.Context, r dto.EvaluateRequest) (dto.RunView, error) {
	var v dto.RunView
	e := h.UOW.Run(ctx, func(ctx context.Context) error { var e error; v, e = h.enqueue(ctx, r); return e })
	return v, e
}
func (h EvaluateAccountHandler) Start(ctx context.Context, r dto.EvaluateRequest) (dto.RunView, error) {
	r.Mutation.Scope = r.Scope
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.RunView, error) { return h.enqueue(ctx, r) })
}
func (h EvaluateAccountHandler) Handle(ctx context.Context, r dto.EvaluateRequest) (dto.EvaluationView, error) {
	frozen, e := h.Prepare(ctx, r)
	if e != nil {
		return dto.EvaluationView{}, e
	}
	if frozen.State != "queued" {
		return dto.EvaluationView{Evaluation: frozen}, nil
	}
	snapshot, e := h.Snapshots.Get(ctx, frozen.DatasetVersion, frozen.SnapshotID)
	if e != nil {
		return dto.EvaluationView{}, e
	}
	universe, e := h.Catalog.GetUniverse(ctx, r.Scope, frozen.UniverseVersionID)
	if e != nil {
		return dto.EvaluationView{}, e
	}
	strategy, e := h.Catalog.GetStrategy(ctx, r.Scope, frozen.StrategyVersionID)
	if e != nil {
		return dto.EvaluationView{}, e
	}
	computed, evalErr := domain.Evaluate(snapshot, universe, strategy)
	computed.ID = frozen.ID
	computed.AccountID = frozen.AccountID
	computed.SessionDate = frozen.SessionDate
	computed.Purpose = frozen.Purpose
	computed.DatasetVersion = frozen.DatasetVersion
	computed.Mode = frozen.Mode
	computed.FrozenPolicy = frozen.FrozenPolicy
	computed.JobID = frozen.JobID
	computed.OrderIDs = []string{}
	if evalErr != nil && !errors.Is(evalErr, domain.ErrInsufficientUniverse) && !errors.Is(evalErr, domain.ErrFactorUnavailable) {
		return dto.EvaluationView{}, evalErr
	}
	var result domain.Evaluation
	e = h.UOW.Run(ctx, func(ctx context.Context) error {
		account, e := h.Accounts.Lock(ctx, r.Scope, frozen.AccountID)
		if e != nil {
			return e
		}
		current, e := h.Runs.GetEvaluation(ctx, r.Scope, frozen.ID)
		if e != nil {
			return e
		}
		if current.State != "queued" {
			result = current
			return nil
		}
		outcome := computed
		now := h.Now()
		session, e := snapshot.Calendar.Session(frozen.SessionDate)
		if e != nil {
			return e
		}
		target, e := snapshot.Calendar.NextSession(session.CloseAt)
		if e != nil {
			return e
		}
		block := ""
		switch {
		case evalErr != nil:
			block = evalErr.Error()
		case frozen.Purpose == "research":
			outcome.Reason = "research_only"
		case !now.Before(target.OpenAt):
			outcome.Reason = "missed_open_research_only"
		case !account.AutomationEnabled:
			block = "automation_paused"
		case account.StrategyVersionID != frozen.StrategyVersionID || account.UniverseVersionID != frozen.UniverseVersionID || account.Policy != frozen.FrozenPolicy:
			block = "configuration_changed"
		case account.PendingConfig != nil && !account.PendingConfig.EffectiveAt.After(target.OpenAt):
			block = "configuration_pending_next_session"
		}
		if block == "" && frozen.Purpose == "automatic" && outcome.Reason == "" {
			issued, e := h.Runs.IssuedOn(ctx, r.Scope, account.ID, frozen.SessionDate)
			if e != nil {
				return e
			}
			if issued {
				block = "daily_order_batch_already_issued"
			}
			book, e := h.Orders.LoadRiskBook(ctx, r.Scope, account.ID, target.Date)
			if e != nil {
				return e
			}
			for _, o := range book.Orders {
				if !o.Terminal() && !now.Before(o.TargetOpenAt) {
					block = "unresolved_effective_orders"
					break
				}
			}
			if block == "" {
				nav, e := domain.ComputeNAV(account, book.Positions, snapshot, nil)
				if e != nil {
					block = e.Error()
				} else {
					plan, e := domain.BuildRebalance(domain.PortfolioInput{Account: account, Positions: book.Positions, Orders: book.Orders, Snapshot: snapshot, Evaluation: outcome, Policy: account.Policy, NAV: nav, PeakNAV: book.PeakNAV, SessionTurnover: book.SessionTurnover, Parameters: &strategy.Parameters})
					if e != nil {
						return e
					}
					if plan.Pause {
						account, e = h.Reservations.CancelBeforeOpen(ctx, account, "drawdown_pause", false)
						if e != nil {
							return e
						}
						account.AutomationEnabled = false
						account.PauseReason = "drawdown_pause"
						if e = h.Accounts.Save(ctx, account, account.Version); e != nil {
							return e
						}
						if e = h.Ledger.InsertAutomationEvent(ctx, r.Scope, account.ID, h.NewID(), "drawdown/"+frozen.ID, "paused", "drawdown_pause", account, now, now); e != nil {
							return e
						}
						block = "drawdown_pause"
					} else if len(plan.Orders) > 0 {
						following, e := snapshot.Calendar.NextSession(target.CloseAt)
						if e != nil {
							return e
						}
						if _, e = snapshot.Calendar.NextSettlement(target.OpenAt); e != nil {
							block = "settlement_calendar_not_configured"
						} else {
							orders := []domain.Order{}
							cashLeft := account.Balances.Available
							for _, planned := range plan.Orders {
								bars := []domain.Bar{}
								close := domain.Price(0)
								for _, bar := range snapshot.Bars {
									if bar.InstrumentID == planned.InstrumentID {
										bars = append(bars, bar)
										if bar.SessionDate.Equal(session.Date) {
											close = bar.Close
										}
									}
								}
								if close <= 0 {
									return domain.ErrDataStale
								}
								capacity, e := domain.PreviousVolumeCapacity(bars, target.OpenAt)
								if e != nil {
									return e
								}
								order := domain.Order{ID: h.NewID(), AccountID: account.ID, InstrumentID: planned.InstrumentID, Side: planned.Side, Quantity: planned.Quantity, State: domain.OrderPending, Reason: planned.Reason, Origin: "automatic", Capacity: capacity, TargetOpenAt: target.OpenAt, ExpiresAt: following.CloseAt, CreatedAt: now, Version: 1}
								if order.Side == "buy" {
									low, high := domain.Quantity(0), order.Quantity
									for low < high {
										mid := low + (high-low+1)/2
										cost, err := domain.ReservationCost(close, mid)
										if err != nil {
											return err
										}
										if cost <= cashLeft {
											low = mid
										} else {
											high = mid - 1
										}
									}
									if low == 0 {
										continue
									}
									order.Quantity = low
									order.ReservedCash, e = domain.ReservationCost(close, order.Quantity)
									if e != nil {
										return e
									}
									cashLeft -= order.ReservedCash
								} else {
									order.ReservedQuantity = order.Quantity
								}
								orders = append(orders, order)
								outcome.OrderIDs = append(outcome.OrderIDs, order.ID)
							}
							if _, e = h.Reservations.Apply(ctx, r.Scope, account, orders); e != nil {
								return e
							}
							outcome.IssuedOrders = len(orders) > 0
						}
					} else if len(plan.Reasons) > 0 {
						block = plan.Reasons[0]
					}
				}
			}
		}
		if block != "" {
			outcome.State = "blocked"
			outcome.Reason = block
		}
		if e = h.Runs.UpdateEvaluation(ctx, r.Scope, account.ID, outcome); e != nil {
			return e
		}
		result = outcome
		return nil
	})
	return dto.EvaluationView{Evaluation: result}, e
}
