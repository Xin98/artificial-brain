package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// ExportDeliveriesHandler pages a workspace's full delivery history — all
// five states and both origins — as export records.
type ExportDeliveriesHandler struct {
	Deliveries ports.DeliveryStore
}

// Handle pages the workspace's delivery history (offset, limit capped at 200)
// as export records, carrying the origin of every row.
func (h *ExportDeliveriesHandler) Handle(ctx context.Context, workspaceID string, offset, limit int) ([]dto.DeliveryExportRecord, error) {
	if limit > maxListLimit {
		limit = maxListLimit
	}
	deliveries, err := h.Deliveries.Export(ctx, workspaceID, offset, limit)
	if err != nil {
		return nil, err
	}
	records := make([]dto.DeliveryExportRecord, 0, len(deliveries))
	for _, delivery := range deliveries {
		records = append(records, toDeliveryExportRecord(delivery))
	}
	return records, nil
}

// toDeliveryExportRecord maps the aggregate to its export row; the zero
// origin exports as the local origin so legacy rows never carry an empty
// origin.
func toDeliveryExportRecord(delivery domain.ReminderDelivery) dto.DeliveryExportRecord {
	origin := delivery.Origin
	if origin == "" {
		origin = domain.OriginLocal
	}
	return dto.DeliveryExportRecord{
		ID:                delivery.ID,
		TodoID:            delivery.TodoID,
		Channel:           delivery.Channel,
		State:             string(delivery.State),
		SuppressionReason: exportSuppressionReason(delivery.SuppressionReason),
		AttemptCount:      delivery.AttemptCount,
		ProviderMessageID: delivery.ProviderMessageID,
		LastErrorCode:     delivery.LastErrorCode,
		TodoTitleSnapshot: delivery.TodoTitleSnapshot,
		ScheduledAt:       delivery.ScheduledAt,
		CreatedAt:         delivery.CreatedAt,
		SubmittedAt:       delivery.SubmittedAt,
		FinalizedAt:       delivery.FinalizedAt,
		ReceiptState:      exportReceiptState(delivery.ReceiptState),
		ReceiptErrorCode:  delivery.ReceiptErrorCode,
		ReceiptAt:         delivery.ReceiptAt,
		Origin:            string(origin),
	}
}

func exportSuppressionReason(reason *domain.SuppressionReason) *string {
	if reason == nil {
		return nil
	}
	value := string(*reason)
	return &value
}

func exportReceiptState(state *domain.ReceiptState) *string {
	if state == nil {
		return nil
	}
	value := string(*state)
	return &value
}
