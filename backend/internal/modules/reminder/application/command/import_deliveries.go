package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// ImportDeliveriesHandler restores an imported delivery as one read-only
// history row. It touches no scheduler and no provider, and performs no
// state-machine transitions: history is history.
type ImportDeliveriesHandler struct {
	Deliveries ports.DeliveryStore
	NewID      func() string
	Now        func() time.Time
}

// Handle writes one read-only history row via SaveImported, idempotent on the
// import key "import:<sourceInstanceID>:<sourceRecordID>"; a duplicate import
// returns domain.ErrDeliveryExists.
func (h *ImportDeliveriesHandler) Handle(ctx context.Context, request dto.ImportDeliveryRequest) error {
	delivery, err := domain.RestoreDelivery(h.NewID(), request.WorkspaceID, request.OwnerUserID, request.TodoID,
		request.TodoReminderVersion, request.Channel, request.TodoTitleSnapshot,
		importIdempotencyKey(request.SourceInstanceID, request.SourceRecordID),
		domain.DeliveryState(request.State),
		importSuppressionReason(request.SuppressionReason), request.AttemptCount,
		request.ProviderMessageID, request.LastErrorCode,
		request.ScheduledAt, request.CreatedAt, request.SubmittedAt, request.FinalizedAt,
		importReceiptState(request.ReceiptState), request.ReceiptAt, request.ReceiptErrorCode)
	if err != nil {
		return err
	}
	return h.Deliveries.SaveImported(ctx, delivery)
}

// importIdempotencyKey builds the caller-scoped key that makes re-importing
// the same source record idempotent.
func importIdempotencyKey(sourceInstanceID, sourceRecordID string) string {
	return "import:" + sourceInstanceID + ":" + sourceRecordID
}

func importSuppressionReason(value *string) *domain.SuppressionReason {
	if value == nil {
		return nil
	}
	reason := domain.SuppressionReason(*value)
	return &reason
}

func importReceiptState(value *string) *domain.ReceiptState {
	if value == nil {
		return nil
	}
	state := domain.ReceiptState(*value)
	return &state
}
