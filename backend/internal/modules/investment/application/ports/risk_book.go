package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type RiskBookReader interface {
	LoadRiskBook(context.Context, domain.Scope, string, time.Time) (dto.RiskBook, error)
}
