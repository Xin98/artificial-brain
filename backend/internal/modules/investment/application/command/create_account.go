package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
	"time"
)

type CreateAccountHandler struct {
	Mutations application.MutationExecutor
	Accounts  ports.AccountStore
	Catalog   ports.CatalogStore
	Ledger    ports.LedgerStore
	Data      ports.ResearchData
	Now       func() time.Time
	NewID     func() string
}

func (h CreateAccountHandler) Handle(ctx context.Context, r dto.CreateAccountRequest) (dto.AccountView, error) {
	cash := domain.Money(10000000)
	if r.InitialCash != "" {
		v, e := domain.ParseMoney(r.InitialCash)
		if e != nil || v <= 0 || v > 100000000000 {
			return dto.AccountView{}, domain.ErrInvalidInput
		}
		cash = v
	}
	if r.Mode == "" {
		r.Mode = "fixture"
	}
	if r.Name == "" {
		r.Name = "美股模拟账户"
	}
	if len([]rune(r.Name)) > 100 || strings.TrimSpace(r.Name) == "" || (r.Mode != "fixture" && r.Mode != "alpaca_sec") {
		return dto.AccountView{}, domain.ErrInvalidInput
	}
	now := h.Now()
	data, e := h.Data.Read(ctx, r.Mode, now)
	if e != nil {
		return dto.AccountView{}, e
	}
	return application.RunMutation(h.Mutations, ctx, r.Mutation, r, func(ctx context.Context) (dto.AccountView, error) {
		a := domain.Account{DatasetVersion: data.DatasetVersion, ID: h.NewID(), Name: r.Name, Mode: r.Mode, Scope: r.Scope, InitialCash: cash, Balances: domain.Balances{Available: cash}, Version: 1, Policy: domain.DefaultRiskPolicy(), CreatedAt: now}
		var u domain.UniverseVersion
		var s domain.StrategyVersion
		var e error
		if r.UniverseVersionID != "" {
			u, e = h.Catalog.GetUniverse(ctx, r.Scope, r.UniverseVersionID)
			if e != nil {
				return dto.AccountView{}, e
			}
			if u.Mode != r.Mode {
				return dto.AccountView{}, domain.ErrInvalidInput
			}
		} else {
			u = domain.UniverseVersion{ID: h.NewID(), UniverseID: h.NewID(), Name: "默认美股股票池", Mode: r.Mode, Scope: r.Scope, CreatedAt: now, EffectiveAt: now, InstrumentIDs: []string{}}
			if r.Mode == "fixture" {
				for _, i := range data.Instruments {
					if i.Kind == "stock" {
						u.InstrumentIDs = append(u.InstrumentIDs, i.ID)
					}
				}
			}
		}
		if r.StrategyVersionID != "" {
			s, e = h.Catalog.GetStrategy(ctx, r.Scope, r.StrategyVersionID)
			if e != nil {
				return dto.AccountView{}, e
			}
		} else {
			s = domain.StrategyVersion{ID: h.NewID(), Scope: r.Scope, StrategyID: "multifactor-v1", Parameters: domain.DefaultStrategyParameters(), CreatedAt: now}
		}
		a.StrategyVersionID = s.ID
		a.UniverseVersionID = u.ID
		if r.UniverseVersionID == "" {
			if e = h.Catalog.InsertUniverse(ctx, r.Scope, u); e != nil {
				return dto.AccountView{}, e
			}
		}
		if r.StrategyVersionID == "" {
			if e = h.Catalog.InsertStrategy(ctx, r.Scope, s); e != nil {
				return dto.AccountView{}, e
			}
		}
		if e = h.Accounts.Insert(ctx, a); e != nil {
			return dto.AccountView{}, e
		}
		e = h.Ledger.InsertLedger(ctx, r.Scope, a.ID, []domain.LedgerEntry{{ID: h.NewID(), AccountID: a.ID, EventKey: "initial_credit", Kind: "initial_credit", Delta: domain.Balances{Available: cash}, EffectiveAt: now, RecordedAt: now}})
		return dto.ViewAccount(a), e
	})
}
