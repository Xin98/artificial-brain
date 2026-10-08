package query

import (
	"context"
	"errors"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"sort"
	"strings"
	"time"
)

type InstrumentsQuery struct {
	Data     ports.ResearchData
	Accounts ports.AccountStore
	Catalog  ports.CatalogStore
	Mode     string
	Now      func() time.Time
}

func (h InstrumentsQuery) Handle(ctx context.Context, r dto.ListRequest) (dto.Page, error) {
	if r.Limit == 0 {
		r.Limit = 25
	}
	if r.Limit < 1 || r.Limit > 100 || len(r.Search) > 100 {
		return dto.Page{}, domain.ErrInvalidInput
	}
	if r.Risk != "" && r.Risk != "low" && r.Risk != "medium" && r.Risk != "high" && r.Risk != "unknown" {
		return dto.Page{}, domain.ErrInvalidInput
	}
	if r.Potential != "" && r.Potential != "high" && r.Potential != "observe" && r.Potential != "weak" && r.Potential != "unknown" {
		return dto.Page{}, domain.ErrInvalidInput
	}
	r.Resource = "instruments"
	after, e := decodeCursor(r)
	if e != nil {
		return dto.Page{}, e
	}
	var a domain.Account
	if r.ParentID != "" {
		a, e = h.Accounts.Get(ctx, r.Scope, r.ParentID)
		if e != nil {
			return dto.Page{}, e
		}
	}
	s, e := h.Data.Read(ctx, h.Mode, h.Now())
	if errors.Is(e, domain.ErrDataNotConfigured) || errors.Is(e, domain.ErrNotFound) {
		return dto.Page{Items: []any{}}, nil
	}
	if e != nil {
		return dto.Page{}, e
	}
	u := domain.UniverseVersion{ID: "research/current-pool"}
	strategy := domain.StrategyVersion{ID: "research/multifactor-v1", Parameters: domain.DefaultStrategyParameters()}
	for _, i := range s.Instruments {
		if i.Kind == "stock" {
			u.InstrumentIDs = append(u.InstrumentIDs, i.ID)
		}
	}
	if r.ParentID != "" {
		if e = domain.ValidateAccountDataset(a, s); e != nil {
			return dto.Page{}, e
		}
		u, e = h.Catalog.GetUniverse(ctx, r.Scope, a.UniverseVersionID)
		if e != nil {
			return dto.Page{}, e
		}
		strategy, e = h.Catalog.GetStrategy(ctx, r.Scope, a.StrategyVersionID)
		if e != nil {
			return dto.Page{}, e
		}
	}
	evaluation, evalErr := domain.Evaluate(s, u, strategy)
	signals := map[string]domain.Signal{}
	excluded := map[string]string{}
	for _, v := range evaluation.Signals {
		signals[v.InstrumentID] = v
	}
	for _, v := range evaluation.Excluded {
		excluded[v.InstrumentID] = v.Reason
	}
	rows := []dto.ListRow{}
	latest, calendarErr := s.Calendar.LatestCompleted(h.Now())
	for _, i := range s.Instruments {
		if r.Search != "" && !strings.Contains(strings.ToUpper(i.Ticker+" "+i.Name), strings.ToUpper(r.Search)) {
			continue
		}
		view := dto.InstrumentResearchView{Instrument: i, Risk: domain.RiskAssessment{Level: "unknown", Reasons: []string{"factor_unavailable"}, AsOf: s.AsOf}, Potential: "unknown", Reason: excluded[i.ID]}
		rank := 999
		if sig, ok := signals[i.ID]; ok {
			copy := sig
			view.Signal = &copy
			view.Risk = sig.Risk
			view.Potential = sig.Recommendation.Potential
			rank = sig.Rank
		} else if evalErr != nil {
			view.Reason = evalErr.Error()
		}
		var last *domain.Bar
		for n, b := range s.Bars {
			if b.InstrumentID == i.ID {
				last = &s.Bars[n]
			}
		}
		if last != nil {
			price := last.Close.String()
			view.Price = &price
			at := last.SessionDate
			view.PriceAsOf = &at
		}
		if domain.Industry(i.SIC) == "unknown" || calendarErr != nil || last == nil || !last.SessionDate.Equal(latest.Date) {
			view.Risk = domain.RiskAssessment{Level: "unknown", Reasons: []string{"industry_or_price_coverage_unknown"}, AsOf: s.AsOf}
		}
		if r.Risk != "" && view.Risk.Level != r.Risk {
			continue
		}
		if r.Potential != "" && view.Potential != r.Potential {
			continue
		}
		key := fmt.Sprintf("%04d/%s", rank, i.ID)
		if after.ID != "" && key <= after.ID {
			continue
		}
		rows = append(rows, dto.ListRow{ID: key, Value: view})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return pageRows(r, rows), nil
}
