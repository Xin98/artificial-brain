package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
	"time"
)

type ListRequest struct {
	Scope                                               domain.Scope
	Resource, ParentID, Cursor, Search, Risk, Potential string
	Limit                                               int
}
type PageCursor struct {
	Scope                         domain.Scope
	Resource, ParentID, Query, ID string
	At                            time.Time
}
type ListRow struct {
	ID    string
	At    time.Time
	Value any
}
type Page struct {
	Items      []any  `json:"items"`
	NextCursor string `json:"nextCursor"`
}
type InstrumentResearchView struct {
	Instrument domain.Instrument
	Signal     *domain.Signal
	Risk       domain.RiskAssessment
	Potential  string
	Reason     string
	Price      *string
	PriceAsOf  *time.Time
}
type RecordedNAV struct {
	Point        domain.NAVPoint
	Turnover     domain.Money
	AvailableAt  time.Time
	QualityFlags []string
}
type AutomationEventView struct {
	ID, Kind, Reason        string
	EffectiveAt, RecordedAt time.Time
}
type PerformanceView struct {
	InitialCash                string
	Mode, Feed, DatasetVersion string
	AsOf                       time.Time
	Curve, BenchmarkCurve      []domain.NAVPoint
	Metrics                    *domain.Performance
	BenchmarkMetrics           *domain.Performance
	QualityFlags               []string
	BenchmarkReason            string
	Kind                       string
}
type BacktestView struct {
	RunView
	Mode, Feed, DatasetVersion, StrategyVersionID, UniverseVersionID, InitialCash string
	From, To                                                                      time.Time
	SnapshotIDs                                                                   []string
	Curve, BenchmarkCurve                                                         []domain.NAVPoint
	Metrics, BenchmarkMetrics                                                     *domain.Performance
	QualityFlags                                                                  []string
	ExecutionModel, BenchmarkReason, IntervalLabel                                string
}

func ViewBacktest(r BacktestRecord) BacktestView {
	feed := ""
	parts := strings.Split(r.DatasetVersion, "/")
	if len(parts) > 1 {
		feed = parts[1]
	}
	v := BacktestView{RunView: r.View, Mode: r.Mode, Feed: feed, DatasetVersion: r.DatasetVersion, StrategyVersionID: r.Request.StrategyVersionID, UniverseVersionID: r.Request.UniverseVersionID, InitialCash: r.Request.InitialCash, From: r.Request.From, To: r.Request.To, SnapshotIDs: r.SnapshotIDs, Curve: []domain.NAVPoint{}, BenchmarkCurve: []domain.NAVPoint{}, QualityFlags: []string{}, ExecutionModel: domain.ExecutionModel, IntervalLabel: "research_not_out_of_sample"}
	if r.Result != nil {
		v.Curve = r.Result.Curve
		v.BenchmarkCurve = r.Result.BenchmarkCurve
		v.Metrics = &r.Result.Metrics
		v.BenchmarkMetrics = r.Result.BenchmarkMetrics
		v.QualityFlags = r.Result.QualityFlags
		v.BenchmarkReason = r.Result.BenchmarkReason
	}
	return v
}
