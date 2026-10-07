package query

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
	"time"
)

type AccountsQuery struct {
	Accounts ports.AccountStore
	Orders   ports.OrderStore
	Store    ports.ReadStore
	Now      func() time.Time
}

func (h AccountsQuery) Handle(ctx context.Context, scope domain.Scope, id string) (dto.AccountView, error) {
	a, e := h.Accounts.Get(ctx, scope, id)
	if e != nil {
		return dto.AccountView{}, e
	}
	v := dto.ViewAccount(a)
	v.AsOf = a.CreatedAt
	v.QualityFlags = []string{}
	v.BlockReasons = []string{}
	parts := strings.Split(a.DatasetVersion, "/")
	if len(parts) > 1 {
		v.Feed = parts[1]
	}
	if a.Mode == "fixture" {
		v.QualityFlags = append(v.QualityFlags, "demonstration_data")
	}
	if !a.AutomationEnabled {
		v.BlockReasons = append(v.BlockReasons, "automation_paused")
	}
	orders, e := h.Orders.ListPending(ctx, scope, id)
	if e != nil {
		return v, e
	}
	unresolved := false
	for _, o := range orders {
		if !h.Now().Before(o.TargetOpenAt) {
			unresolved = true
			v.BlockReasons = append(v.BlockReasons, "awaiting_daily_confirmation")
			break
		}
	}
	last, e := h.Store.LatestNAV(ctx, scope, id)
	if e != nil {
		return v, e
	}
	if last != nil {
		n := last.Point.NAV.String()
		v.NAV = &n
		v.AsOf = last.AvailableAt
		v.QualityFlags = append(v.QualityFlags, last.QualityFlags...)
	} else {
		positions, e := h.Orders.LoadPositions(ctx, scope, id)
		if e != nil {
			return v, e
		}
		held := false
		for _, p := range positions {
			if p.Quantity > 0 {
				held = true
			}
		}
		if !held && !unresolved {
			cash, e := a.Balances.Total()
			if e != nil {
				return v, e
			}
			n := cash.String()
			v.NAV = &n
		} else {
			v.BlockReasons = append(v.BlockReasons, "nav_not_yet_confirmed")
		}
	}
	return v, nil
}
