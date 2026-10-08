package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type FinancialDataPort interface {
	Facts(context.Context, []string, time.Time) ([]domain.FinancialFact, error)
	Company(context.Context, string) (domain.Instrument, error)
}
type FinancialIdentityPort interface {
	Identify(context.Context, []domain.Instrument) ([]domain.Instrument, error)
}
type FinancialBatchStore interface {
	AppendFinancials(context.Context, string, []domain.FinancialFact) error
}
type InstrumentMetadataStore interface {
	AppendInstruments(context.Context, string, []domain.Instrument, time.Time) error
}
