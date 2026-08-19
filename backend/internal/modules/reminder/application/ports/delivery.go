package ports

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// DeliveryStore persists reminder deliveries. Implementations resolve their
// executor from context so writes join the caller's ambient transaction.
type DeliveryStore interface {
	// Save inserts a delivery; a duplicate idempotency key returns
	// domain.ErrDeliveryExists.
	Save(ctx context.Context, delivery domain.ReminderDelivery) error
	// Update replaces a delivery's mutable fields; final deliveries are the
	// caller's invariant to guard.
	Update(ctx context.Context, delivery domain.ReminderDelivery) error
	// ByIdempotencyKey loads one delivery scoped by workspace; a missing row
	// returns domain.ErrDeliveryNotFound.
	ByIdempotencyKey(ctx context.Context, workspaceID, key string) (domain.ReminderDelivery, error)
	// ByProviderMessageID loads one delivery by its provider message ID
	// (D6 provider-keyed); a missing row returns domain.ErrDeliveryNotFound.
	ByProviderMessageID(ctx context.Context, providerMessageID string) (domain.ReminderDelivery, error)
	// SetProviderJobID writes the scheduler-assigned job ID back onto the
	// delivery row.
	SetProviderJobID(ctx context.Context, workspaceID, deliveryID string, jobID int64) error
	// PlannedJobIDs returns the provider job IDs of every scheduled delivery
	// for the todo at or below the reminder version cutoff.
	PlannedJobIDs(ctx context.Context, workspaceID, todoID string, upToReminderVersion int) ([]int64, error)
	// Stats counts deliveries per lifecycle bucket for the workspace.
	Stats(ctx context.Context, workspaceID string) (dto.DeliveryCounts, error)
	// List returns deliveries for the workspace matching the filter.
	List(ctx context.Context, workspaceID string, filter dto.DeliveryFilter) ([]domain.ReminderDelivery, error)
}
