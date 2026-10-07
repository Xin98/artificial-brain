package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
)

type RiskBook struct {
	Positions                []domain.Position
	Orders                   []domain.Order
	PeakNAV, SessionTurnover domain.Money
}
