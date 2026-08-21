package query

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

var errExportFailed = errors.New("export failed")

func strPtr(value string) *string        { return &value }
func timePtr(value time.Time) *time.Time { return &value }

// newExportDelivery builds a succeeded delivery with receipt and historical
// timestamps, carrying the given origin, so every mapped field is exercised.
func newExportDelivery(t *testing.T, id string, origin domain.DeliveryOrigin, createdAt time.Time) domain.ReminderDelivery {
	t.Helper()
	delivery, err := domain.NewDelivery(id, "ws-1", "user-1", "todo-"+id, 1, "plan-"+id, "email",
		"review the launch checklist", createdAt.Add(-time.Hour), createdAt)
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	if err := delivery.MarkSending(createdAt); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := delivery.MarkSucceeded("provider-message-"+id, createdAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if err := delivery.ApplyReceipt(domain.ReceiptOK, "", createdAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("ApplyReceipt() error = %v", err)
	}
	delivery.Origin = origin
	return delivery
}

func TestExportDeliveriesCapsLimitAt200(t *testing.T) {
	tests := []struct {
		name      string
		offset    int
		limit     int
		wantLimit int
	}{
		{"limit capped at max", 0, 300, 200},
		{"limit at the cap passes through", 10, 200, 200},
		{"limit inside bounds passes through", 10, 50, 50},
		{"zero limit passes through", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeDeliveryStore()
			handler := &ExportDeliveriesHandler{Deliveries: store}

			if _, err := handler.Handle(context.Background(), "ws-1", tt.offset, tt.limit); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if len(store.exportCalls) != 1 {
				t.Fatalf("export calls = %d, want 1", len(store.exportCalls))
			}
			call := store.exportCalls[0]
			if call.workspaceID != "ws-1" || call.offset != tt.offset || call.limit != tt.wantLimit {
				t.Fatalf("export call = %#v, want workspace ws-1 offset %d limit %d", call, tt.offset, tt.wantLimit)
			}
		})
	}
}

func TestExportDeliveriesMapsDomainToRecord(t *testing.T) {
	store := newFakeDeliveryStore()
	imported := newExportDelivery(t, "delivery-1", domain.OriginImported, fixedNow.Add(-2*time.Hour))
	legacy := newExportDelivery(t, "delivery-2", "", fixedNow.Add(-time.Hour))
	store.exportRows = []domain.ReminderDelivery{imported, legacy}
	handler := &ExportDeliveriesHandler{Deliveries: store}

	records, err := handler.Handle(context.Background(), "ws-1", 0, 50)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}

	createdAt := fixedNow.Add(-2 * time.Hour)
	wantImported := dto.DeliveryExportRecord{
		ID:                "delivery-1",
		TodoID:            "todo-delivery-1",
		Channel:           "email",
		State:             "succeeded",
		SuppressionReason: nil,
		AttemptCount:      1,
		ProviderMessageID: strPtr("provider-message-delivery-1"),
		LastErrorCode:     nil,
		TodoTitleSnapshot: "review the launch checklist",
		ScheduledAt:       createdAt.Add(-time.Hour),
		CreatedAt:         createdAt,
		SubmittedAt:       timePtr(createdAt.Add(time.Minute)),
		FinalizedAt:       timePtr(createdAt.Add(time.Minute)),
		ReceiptState:      strPtr("received_ok"),
		ReceiptErrorCode:  nil,
		ReceiptAt:         timePtr(createdAt.Add(2 * time.Minute)),
		Origin:            "imported",
	}
	if !reflect.DeepEqual(records[0], wantImported) {
		t.Fatalf("records[0] = %#v, want %#v", records[0], wantImported)
	}

	// The zero origin exports as the local origin so legacy rows never carry
	// an empty origin.
	if records[1].ID != "delivery-2" || records[1].Origin != "local" {
		t.Fatalf("records[1] id/origin = %q/%q, want delivery-2/local", records[1].ID, records[1].Origin)
	}
	if records[1].State != "succeeded" || records[1].AttemptCount != 1 || records[1].TodoTitleSnapshot != "review the launch checklist" {
		t.Fatalf("records[1] = %#v, want the legacy delivery mapped", records[1])
	}
}

func TestExportDeliveriesReturnsEmptySliceWithoutRows(t *testing.T) {
	store := newFakeDeliveryStore()
	handler := &ExportDeliveriesHandler{Deliveries: store}

	records, err := handler.Handle(context.Background(), "ws-1", 0, 50)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if records == nil || len(records) != 0 {
		t.Fatalf("records = %#v, want an empty slice", records)
	}
}

func TestExportDeliveriesPropagatesStoreError(t *testing.T) {
	store := newFakeDeliveryStore()
	store.exportErr = errExportFailed
	handler := &ExportDeliveriesHandler{Deliveries: store}

	if _, err := handler.Handle(context.Background(), "ws-1", 0, 50); !errors.Is(err, errExportFailed) {
		t.Fatalf("Handle() error = %v, want errExportFailed", err)
	}
}
