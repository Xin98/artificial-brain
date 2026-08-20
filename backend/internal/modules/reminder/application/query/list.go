// Package query implements the Reminder application read side: delivery
// stats, the delivery listing, and the instance-wide ops snapshot.
package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// List pagination bounds; validation lives here so the store's List stays a
// dumb passthrough.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// ListDeliveriesHandler lists a workspace's reminder deliveries with status
// filtering and pagination.
type ListDeliveriesHandler struct {
	Deliveries ports.DeliveryStore
}

// Handle returns the matching deliveries. Limit defaults to 50 and is capped
// at 200; offset is clamped to non-negative; the status filter is passed
// through untouched (the store owns the 'retrying' alias).
func (h *ListDeliveriesHandler) Handle(ctx context.Context, workspaceID string, filter dto.DeliveryFilter) ([]domain.ReminderDelivery, error) {
	if filter.Limit <= 0 {
		filter.Limit = defaultListLimit
	}
	if filter.Limit > maxListLimit {
		filter.Limit = maxListLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return h.Deliveries.List(ctx, workspaceID, filter)
}
