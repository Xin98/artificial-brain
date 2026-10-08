package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

// Read returns already ingested data; it never performs external HTTP in a mutation.
type ResearchData interface {
	Read(context.Context, string, time.Time) (domain.Snapshot, error)
}
type AutomationEventStore interface {
	InsertAutomationEvent(context.Context, domain.Scope, string, string, string, string, string, any, time.Time, time.Time) error
}
