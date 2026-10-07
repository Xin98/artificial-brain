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
type AnalysisView struct {
	AccountRisk                *domain.RiskDecision
	Instrument                 domain.Instrument
	AsOf                       time.Time
	Mode, Feed, DatasetVersion string
	Metrics                    domain.FinancialMetrics
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
	Mode, Feed, DatasetVersion        string
	AsOf                              time.Time
	Market, Financial, News, Calendar DataComponentStatus
	QualityFlags                      []string
}
