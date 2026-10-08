package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type AnalysisRequest struct {
	Scope                   domain.Scope
	InstrumentID, AccountID string
	AsOf                    time.Time
}
type PricePoint struct {
	SessionDate time.Time
	Close       domain.Price
}
type AnalysisView struct {
	AccountRisk                *domain.RiskDecision
	Instrument                 domain.Instrument
	AsOf                       time.Time
	Mode, Feed, DatasetVersion string
	Metrics                    domain.FinancialMetrics
	Prices                     []PricePoint
	Signal                     *domain.Signal
	Risk                       domain.RiskAssessment
	Recommendation             domain.Recommendation
	NewsStatus, NewsReason     string
	Topics                     []domain.Topic
	QualityFlags               []string
}
type DataComponentStatus struct {
	State, Reason  string
	Count          int
	From, To, AsOf time.Time
}
type DataStatus struct {
	LastSync                          *RunView
	Mode, Feed, DatasetVersion        string
	AsOf                              time.Time
	Market, Financial, News, Calendar DataComponentStatus
	QualityFlags                      []string
}
