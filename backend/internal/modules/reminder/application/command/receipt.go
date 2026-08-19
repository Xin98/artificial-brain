package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// ReceiptRequest is the provider delivery-receipt verdict for one sent
// message, already parsed and signature-checked by the inbound webhook.
type ReceiptRequest struct {
	ProviderMessageID string
	Delivered         bool
	ErrorCode         string
}

// RecordReceiptHandler records a provider delivery receipt. Receipts are
// informational: they never change a delivery's terminal state, they apply
// only to succeeded deliveries, and the first receipt wins.
type RecordReceiptHandler struct {
	Deliveries ports.DeliveryStore
	Log        *slog.Logger
	Now        func() time.Time
}

// Handle applies the receipt to the delivery identified by the provider
// message id (D6 provider-keyed, not workspace-scoped). Unknown provider ids
// and non-succeeded deliveries are safely ignored and logged.
func (h *RecordReceiptHandler) Handle(ctx context.Context, request ReceiptRequest) error {
	delivery, err := h.Deliveries.ByProviderMessageID(ctx, request.ProviderMessageID)
	if err != nil {
		if errors.Is(err, domain.ErrDeliveryNotFound) {
			h.Log.Info("reminder receipt ignored: unknown provider message id",
				slog.String("providerMessageId", request.ProviderMessageID),
			)
			return nil
		}
		return err
	}
	if delivery.State != domain.StateSucceeded {
		h.Log.Info("reminder receipt ignored: delivery not succeeded",
			slog.String("providerMessageId", request.ProviderMessageID),
			slog.String("state", string(delivery.State)),
		)
		return nil
	}
	if delivery.ReceiptState != nil {
		return nil
	}
	state := domain.ReceiptOK
	if !request.Delivered {
		state = domain.ReceiptFailed
	}
	if err := delivery.ApplyReceipt(state, request.ErrorCode, h.Now()); err != nil {
		if errors.Is(err, domain.ErrReceiptNotApplicable) {
			return nil
		}
		return err
	}
	return h.Deliveries.Update(ctx, delivery)
}
