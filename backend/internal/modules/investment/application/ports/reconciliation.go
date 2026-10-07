package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type ReconciliationStore interface {
	LedgerStore
	ClaimEvent(context.Context, domain.Scope, string, string) (bool, error)
	Unsettled(context.Context, domain.Scope, string) ([]domain.LedgerEntry, error)
	ActionEligibility(context.Context, domain.Scope, string, string) ([]domain.Position, error)
	SaveActionEligibility(context.Context, domain.Scope, string, domain.CorporateAction, []domain.Position) error
	RecordedAction(context.Context, domain.Scope, string, string) (domain.CorporateAction, error)
	EventEntry(context.Context, domain.Scope, string, string) (domain.LedgerEntry, error)
	InventoryChangedSince(context.Context, domain.Scope, string, string, time.Time) (bool, error)
}
