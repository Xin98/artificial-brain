package query

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
	"time"
)

type PerformanceQuery struct {
	Accounts ports.AccountStore
	Store    ports.ReadStore
	History  ports.HistoryData
	Now      func() time.Time
}

func (h PerformanceQuery) Handle(ctx context.Context, scope domain.Scope, id string) (dto.PerformanceView, error) {
	a, e := h.Accounts.Get(ctx, scope, id)
	if e != nil {
		return dto.PerformanceView{}, e
	}
	v := dto.PerformanceView{Mode: a.Mode, DatasetVersion: a.DatasetVersion, Curve: []domain.NAVPoint{}, BenchmarkCurve: []domain.NAVPoint{}, QualityFlags: []string{}, Kind: "forward_paper", BenchmarkReason: "benchmark_history_incomplete"}
	parts := strings.Split(a.DatasetVersion, "/")
	if len(parts) > 1 {
		v.Feed = parts[1]
	}
	history, e := h.Store.NAVHistory(ctx, scope, id)
	if e != nil {
		return v, e
	}
	for _, p := range history {
		v.Curve = append(v.Curve, p.Point)
		v.AsOf = p.AvailableAt
		v.QualityFlags = append(v.QualityFlags, p.QualityFlags...)
	}
	if len(v.Curve) == 0 {
		v.QualityFlags = append(v.QualityFlags, "nav_not_yet_confirmed")
		return v, nil
	}
	fills, e := h.Store.PerformanceFills(ctx, scope, id)
	if e != nil {
		return v, e
	}
	p, e := domain.ComputePerformance(v.Curve, fills, a.InitialCash)
	if e != nil {
		return v, e
	}
	v.Metrics = &p
	if h.History != nil {
		s, err := h.History.History(ctx, a.Mode, h.Now())
		if err == nil && s.DatasetVersion == a.DatasetVersion {
			benchmark, metrics, err := domain.BenchmarkSeries(ctx, s, a.CreatedAt, v.Curve, a.InitialCash)
			if err == nil {
				v.BenchmarkCurve = benchmark
				v.BenchmarkMetrics = &metrics
				v.BenchmarkReason = ""
			}
		} else if err != nil && err != domain.ErrDataNotConfigured && err != domain.ErrNotFound {
			return v, err
		}
	}
	return v, nil
}
