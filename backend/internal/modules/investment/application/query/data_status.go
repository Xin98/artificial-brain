package query

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type DataStatusQuery struct {
	Data        ports.ResearchData
	Mode        string
	NewsEnabled bool
	Now         func() time.Time
}

func (h DataStatusQuery) Handle(ctx context.Context, scope domain.Scope, mode string) (dto.DataStatus, error) {
	out := dto.DataStatus{Mode: mode, QualityFlags: []string{}}
	absent := dto.DataComponentStatus{State: "not_configured", Reason: "data_not_configured"}
	out.Market = absent
	out.Financial = absent
	out.News = absent
	out.Calendar = absent
	if mode != h.Mode {
		return out, nil
	}
	s, e := h.Data.Read(ctx, mode, h.Now())
	if errors.Is(e, domain.ErrNotFound) || errors.Is(e, domain.ErrDataNotConfigured) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	out.Mode = s.Mode
	out.Feed = s.Feed
	out.DatasetVersion = s.DatasetVersion
	out.AsOf = s.AsOf
	out.QualityFlags = s.QualityFlags
	out.Market = coverage(len(s.Bars), s.AsOf)
	for _, b := range s.Bars {
		if out.Market.From.IsZero() || b.SessionDate.Before(out.Market.From) {
			out.Market.From = b.SessionDate
		}
		if b.SessionDate.After(out.Market.To) {
			out.Market.To = b.SessionDate
		}
	}
	out.Financial = coverage(len(s.Facts), s.AsOf)
	for _, f := range s.Facts {
		if out.Financial.From.IsZero() || f.PeriodEnd.Before(out.Financial.From) {
			out.Financial.From = f.PeriodEnd
		}
		if f.PeriodEnd.After(out.Financial.To) {
			out.Financial.To = f.PeriodEnd
		}
	}
	out.News = coverage(len(s.News), s.AsOf)
	if mode != "fixture" && !h.NewsEnabled {
		out.News = absent
		out.News.Reason = "news_permission_not_verified"
	}
	out.Calendar = coverage(len(s.Calendar.Sessions), s.AsOf)
	out.Calendar.From = s.Calendar.CoverageStart
	out.Calendar.To = s.Calendar.CoverageEnd
	if len(s.Calendar.SettlementDays) == 0 {
		out.Calendar.State = "blocked"
		out.Calendar.Reason = "settlement_calendar_not_configured"
	}
	for _, flag := range s.QualityFlags {
		if flag == "news_unavailable" {
			out.News.State = "unavailable"
			out.News.Reason = flag
		}
		if flag == "news_not_configured" {
			out.News.State = "not_configured"
			out.News.Reason = flag
		}
	}
	return out, nil
}
func coverage(n int, asOf time.Time) dto.DataComponentStatus {
	state := "available"
	reason := "coverage_is_not_completeness"
	if n == 0 {
		state = "missing"
		reason = "no_records"
	}
	return dto.DataComponentStatus{State: state, Reason: reason, Count: n, AsOf: asOf}
}
