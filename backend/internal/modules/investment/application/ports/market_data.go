package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type MarketDataPort interface {
	Instruments(context.Context, []string) ([]domain.Instrument, error)
	Bars(context.Context, []string, time.Time, time.Time) ([]domain.Bar, error)
	Calendar(context.Context, time.Time, time.Time) (domain.Calendar, error)
	Actions(context.Context, []string, time.Time, time.Time) ([]domain.CorporateAction, error)
}
type MarketBatchStore interface {
	AppendMarket(context.Context, string, dto.MarketBatch) error
}
