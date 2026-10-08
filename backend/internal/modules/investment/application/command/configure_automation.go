package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type ConfigureAutomationHandler struct {
	Mutations    application.MutationExecutor
	Accounts     ports.AccountStore
	Catalog      ports.CatalogStore
	Events       ports.AutomationEventStore
	Data         ports.ResearchData
	Now          func() time.Time
	NewID        func() string
	Reservations *ReservationWriter
}

func (h ConfigureAutomationHandler) Handle(ctx context.Context, r dto.ConfigureAutomationRequest) (dto.AccountView, error) {
	if r.ExpectedVersion < 1 {
		return dto.AccountView{}, domain.ErrInvalidInput
	}
	if e := r.Policy.Validate(); e != nil {
		return dto.AccountView{}, e
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.AccountView, error) {
		a, e := h.Accounts.Lock(ctx, r.Scope, r.AccountID)
		if e != nil {
			return dto.AccountView{}, e
		}
		if a.Version != r.ExpectedVersion {
			return dto.AccountView{}, domain.ErrVersionConflict
		}
		if r.Mode != "" && r.Mode != a.Mode {
			return dto.AccountView{}, domain.ErrInvalidInput
		}
		u, e := h.Catalog.GetUniverse(ctx, r.Scope, r.UniverseVersionID)
		if e != nil {
			return dto.AccountView{}, e
		}
		if u.Mode != a.Mode {
			return dto.AccountView{}, domain.ErrInvalidInput
		}
		if _, e = h.Catalog.GetStrategy(ctx, r.Scope, r.StrategyVersionID); e != nil {
			return dto.AccountView{}, e
		}
		now := h.Now()
		data, e := h.Data.Read(ctx, a.Mode, now)
		if e != nil && r.Enabled {
			return dto.AccountView{}, e
		}
		if r.Enabled {
			if e = domain.ValidateAccountDataset(a, data); e != nil {
				return dto.AccountView{}, e
			}
		}
		var next domain.Session
		changed := r.UniverseVersionID != a.UniverseVersionID || r.StrategyVersionID != a.StrategyVersionID || r.Policy != a.Policy
		if r.Enabled || changed {
			next, e = data.Calendar.NextSession(now)
			if e != nil {
				return dto.AccountView{}, e
			}
			if _, e = data.Calendar.NextSettlement(next.OpenAt); e != nil {
				return dto.AccountView{}, domain.ErrDataNotConfigured
			}
		}
		if changed {
			a.PendingConfig = &domain.AccountConfig{StrategyVersionID: r.StrategyVersionID, UniverseVersionID: r.UniverseVersionID, Policy: r.Policy, EffectiveAt: next.OpenAt}
		}
		if h.Reservations != nil && (!r.Enabled || changed) {
			reason := "user_paused"
			if r.Enabled {
				reason = "configuration_changed"
			}
			a, e = h.Reservations.CancelBeforeOpen(ctx, a, reason, r.Enabled)
			if e != nil {
				return dto.AccountView{}, e
			}
		}
		a.AutomationEnabled = r.Enabled
		a.PauseReason = ""
		if !r.Enabled {
			a.PauseReason = "user_paused"
		}
		if e = h.Accounts.Save(ctx, a, r.ExpectedVersion); e != nil {
			return dto.AccountView{}, e
		}
		a.Version++
		kind := "enabled"
		if !r.Enabled {
			kind = "paused"
		}
		e = h.Events.InsertAutomationEvent(ctx, r.Scope, a.ID, h.NewID(), "configuration/"+r.Key, kind, a.PauseReason, a, now, now)
		return dto.ViewAccount(a), e
	})
}
