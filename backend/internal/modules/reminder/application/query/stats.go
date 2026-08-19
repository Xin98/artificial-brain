package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

// DeliveryStatsHandler counts a workspace's reminder deliveries by lifecycle
// state, powering the dashboard reminder counters.
type DeliveryStatsHandler struct {
	Deliveries ports.DeliveryStore
}

// Handle returns the delivery counts for the workspace.
func (h *DeliveryStatsHandler) Handle(ctx context.Context, workspaceID string) (dto.DeliveryCounts, error) {
	return h.Deliveries.Stats(ctx, workspaceID)
}
