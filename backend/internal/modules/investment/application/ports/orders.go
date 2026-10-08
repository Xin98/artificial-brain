package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type OrderStore interface {
	RiskBookReader
	InsertOrder(context.Context, domain.Scope, string, domain.Order) error
	GetOrder(context.Context, domain.Scope, string, string) (domain.Order, error)
	LockOrder(context.Context, domain.Scope, string, string) (domain.Order, error)
	ListPending(context.Context, domain.Scope, string) ([]domain.Order, error)
	SaveOrder(context.Context, domain.Scope, string, domain.Order, int) error
	InsertFill(context.Context, domain.Scope, string, domain.Fill) error
	GetFill(context.Context, domain.Scope, string, string) (domain.Fill, error)
	LoadPositions(context.Context, domain.Scope, string) ([]domain.Position, error)
	SavePositions(context.Context, domain.Scope, string, []domain.Position) error
	SessionCapacityUsed(context.Context, domain.Scope, string, string, time.Time) (domain.Quantity, error)
	SessionCapacityLimit(context.Context, domain.Scope, string, string, time.Time) (domain.Quantity, error)
}
