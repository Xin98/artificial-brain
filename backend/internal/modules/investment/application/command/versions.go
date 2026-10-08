package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type CreateUniverseVersionHandler struct {
	Mutations application.MutationExecutor
	Catalog   ports.CatalogStore
	Data      ports.ResearchData
	Now       func() time.Time
	NewID     func() string
}

func (h CreateUniverseVersionHandler) Handle(ctx context.Context, r dto.CreateUniverseVersionRequest) (dto.VersionView, error) {
	if len(r.InstrumentIDs) > 100 || len([]rune(r.Name)) < 1 || len([]rune(r.Name)) > 100 {
		return dto.VersionView{}, domain.ErrInvalidInput
	}
	if r.Mode == "" {
		r.Mode = "fixture"
	}
	seen := map[string]bool{}
	for _, id := range r.InstrumentIDs {
		if seen[id] || id == "" {
			return dto.VersionView{}, domain.ErrInvalidInput
		}
		seen[id] = true
	}
	now := h.Now()
	s, e := h.Data.Read(ctx, r.Mode, now)
	if e != nil {
		return dto.VersionView{}, e
	}
	known := map[string]domain.Instrument{}
	for _, i := range s.Instruments {
		known[i.ID] = i
	}
	for _, id := range r.InstrumentIDs {
		i, ok := known[id]
		if !ok || i.Kind != "stock" {
			return dto.VersionView{}, domain.ErrInvalidInput
		}
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.VersionView, error) {
		parent := r.UniverseID
		if parent != "" {
			old, e := h.Catalog.GetUniverse(ctx, r.Scope, parent)
			if e != nil {
				return dto.VersionView{}, e
			}
			if old.Mode != r.Mode {
				return dto.VersionView{}, domain.ErrInvalidInput
			}
			parent = old.UniverseID
		} else {
			parent = h.NewID()
		}
		v := domain.UniverseVersion{ID: h.NewID(), UniverseID: parent, Name: r.Name, Mode: r.Mode, Scope: r.Scope, InstrumentIDs: append([]string(nil), r.InstrumentIDs...), EffectiveAt: now, CreatedAt: now}
		e := h.Catalog.InsertUniverse(ctx, r.Scope, v)
		return dto.VersionView{ID: v.ID, ParentID: v.UniverseID, Name: v.Name, Mode: v.Mode, CreatedAt: now, EffectiveAt: now, InstrumentIDs: v.InstrumentIDs}, e
	})
}

type CreateStrategyVersionHandler struct {
	Mutations application.MutationExecutor
	Catalog   ports.CatalogStore
	Now       func() time.Time
	NewID     func() string
}

func (h CreateStrategyVersionHandler) Handle(ctx context.Context, r dto.CreateStrategyVersionRequest) (dto.VersionView, error) {
	if e := r.Parameters.Validate(); e != nil {
		return dto.VersionView{}, e
	}
	if r.StrategyID == "" {
		r.StrategyID = "multifactor-v1"
	}
	if r.StrategyID != "multifactor-v1" {
		return dto.VersionView{}, domain.ErrInvalidInput
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.VersionView, error) {
		now := h.Now()
		v := domain.StrategyVersion{ID: h.NewID(), StrategyID: r.StrategyID, Scope: r.Scope, Parameters: r.Parameters, CreatedAt: now}
		e := h.Catalog.InsertStrategy(ctx, r.Scope, v)
		return dto.VersionView{ID: v.ID, ParentID: v.StrategyID, Name: "可解释多因子 v1", CreatedAt: now, Parameters: &v.Parameters}, e
	})
}
