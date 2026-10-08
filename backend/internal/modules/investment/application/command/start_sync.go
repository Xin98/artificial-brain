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

type StartSyncRequest struct {
	dto.Mutation
	InstrumentIDs []string  `json:"instrumentIds"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
}
type StartSyncHandler struct {
	Mutations            application.MutationExecutor
	Runs                 ports.SyncRunStore
	Scheduler            ports.JobScheduler
	Mode, DatasetVersion string
	Now                  func() time.Time
	NewID                func() string
}

func (h StartSyncHandler) Handle(ctx context.Context, r StartSyncRequest) (dto.RunView, error) {
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.RunView, error) {
		now := h.Now()
		if len(r.InstrumentIDs) > 100 || !r.To.After(r.From) || r.To.After(now) || r.From.Before(now.AddDate(-5, 0, 0)) {
			return dto.RunView{}, domain.ErrInvalidInput
		}
		seen := map[string]bool{}
		for _, id := range r.InstrumentIDs {
			if id == "" || seen[id] {
				return dto.RunView{}, domain.ErrInvalidInput
			}
			seen[id] = true
		}
		if h.Mode != "fixture" && len(r.InstrumentIDs) == 0 {
			return dto.RunView{}, domain.ErrInvalidInput
		}
		v := dto.RunView{ID: h.NewID(), Status: "queued", Phase: "market", CreatedAt: now, UpdatedAt: now}
		run := dto.SyncRun{Scope: r.Scope, View: v, Request: dto.SyncRequest{RunID: v.ID, Mode: h.Mode, DatasetVersion: h.DatasetVersion, InstrumentIDs: r.InstrumentIDs, From: r.From, To: r.To}}
		if e := h.Runs.InsertSync(ctx, run); e != nil {
			return v, fmt.Errorf("persist sync: %w", e)
		}
		_, e := h.Scheduler.Enqueue(ctx, dto.InvestmentJobArgs{JobType: "sync", RunID: v.ID, WorkspaceID: r.Scope.WorkspaceID, OwnerUserID: r.Scope.OwnerUserID})
		if e != nil {
			return v, fmt.Errorf("schedule sync: %w", e)
		}
		return v, nil
	})
}
