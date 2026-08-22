package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

func newImportDeliveriesHandler(store *fakeDeliveryStore) *ImportDeliveriesHandler {
	return &ImportDeliveriesHandler{
		Deliveries: store,
		NewID:      func() string { return "delivery-import-1" },
		Now:        func() time.Time { return fixedNow },
	}
}

func importDeliveryRequest() dto.ImportDeliveryRequest {
	messageID := "provider-message-7"
	submitted := fixedNow.Add(-time.Hour)
	finalized := submitted
	return dto.ImportDeliveryRequest{
		WorkspaceID:         "ws-1",
		OwnerUserID:         "user-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 2,
		Channel:             "email",
		State:               "succeeded",
		ProviderMessageID:   &messageID,
		AttemptCount:        1,
		TodoTitleSnapshot:   "提交周报",
		ScheduledAt:         fixedNow.Add(-2 * time.Hour),
		CreatedAt:           fixedNow.Add(-3 * time.Hour),
		SubmittedAt:         &submitted,
		FinalizedAt:         &finalized,
		SourceInstanceID:    "instance-a",
		SourceRecordID:      "record-1",
	}
}

func TestImportDeliveriesRestoresOneReadOnlyHistoryRow(t *testing.T) {
	store := newFakeDeliveryStore()
	handler := newImportDeliveriesHandler(store)

	if err := handler.Handle(context.Background(), importDeliveryRequest()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.imported) != 1 {
		t.Fatalf("imported deliveries = %d, want 1", len(store.imported))
	}
	delivery := store.imported[0]
	if delivery.ID != "delivery-import-1" || delivery.WorkspaceID != "ws-1" || delivery.OwnerUserID != "user-1" || delivery.TodoID != "todo-1" {
		t.Fatalf("delivery identity = %#v", delivery)
	}
	if delivery.TodoReminderVersion != 2 || delivery.Channel != "email" || delivery.TodoTitleSnapshot != "提交周报" {
		t.Fatalf("delivery history fields = %#v", delivery)
	}
	if delivery.IdempotencyKey != "import:instance-a:record-1" {
		t.Fatalf("delivery.IdempotencyKey = %q, want %q", delivery.IdempotencyKey, "import:instance-a:record-1")
	}
	if delivery.State != domain.StateSucceeded || delivery.AttemptCount != 1 {
		t.Fatalf("delivery state/attempts = %q/%d, want succeeded/1", delivery.State, delivery.AttemptCount)
	}
	if delivery.ProviderMessageID == nil || *delivery.ProviderMessageID != "provider-message-7" {
		t.Fatalf("delivery.ProviderMessageID = %v, want provider-message-7", delivery.ProviderMessageID)
	}
	if !delivery.ScheduledAt.Equal(fixedNow.Add(-2*time.Hour)) || !delivery.CreatedAt.Equal(fixedNow.Add(-3*time.Hour)) {
		t.Fatalf("delivery schedule/created = %v/%v", delivery.ScheduledAt, delivery.CreatedAt)
	}
	if delivery.SubmittedAt == nil || !delivery.SubmittedAt.Equal(fixedNow.Add(-time.Hour)) || delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(fixedNow.Add(-time.Hour)) {
		t.Fatalf("delivery submitted/finalized = %v/%v", delivery.SubmittedAt, delivery.FinalizedAt)
	}
	if delivery.PlanID != "" {
		t.Fatalf("delivery.PlanID = %q, want empty for an imported delivery", delivery.PlanID)
	}
	if delivery.Origin != domain.OriginImported {
		t.Fatalf("delivery.Origin = %q, want %q", delivery.Origin, domain.OriginImported)
	}
	// The import path never touches the plan-time write paths.
	if len(store.saved) != 0 || len(store.updated) != 0 {
		t.Fatalf("plan-time writes = %d saves, %d updates, want none", len(store.saved), len(store.updated))
	}
}

func TestImportDeliveriesRestoresSuppressedHistoryWithReceiptless(t *testing.T) {
	store := newFakeDeliveryStore()
	handler := newImportDeliveriesHandler(store)

	request := importDeliveryRequest()
	request.State = "suppressed"
	request.SourceRecordID = "record-2"
	request.ProviderMessageID = nil
	request.SubmittedAt = nil
	request.FinalizedAt = nil
	reason := "version_stale"
	request.SuppressionReason = &reason

	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.imported) != 1 {
		t.Fatalf("imported deliveries = %d, want 1", len(store.imported))
	}
	delivery := store.imported[0]
	if delivery.State != domain.StateSuppressed {
		t.Fatalf("delivery.State = %q, want %q", delivery.State, domain.StateSuppressed)
	}
	if delivery.SuppressionReason == nil || *delivery.SuppressionReason != domain.ReasonVersionStale {
		t.Fatalf("delivery.SuppressionReason = %v, want %q", delivery.SuppressionReason, domain.ReasonVersionStale)
	}
	if delivery.ProviderMessageID != nil || delivery.SubmittedAt != nil || delivery.FinalizedAt != nil {
		t.Fatalf("delivery unset optionals not nil = %#v", delivery)
	}
}

func TestImportDeliveriesRestoresReceiptFields(t *testing.T) {
	store := newFakeDeliveryStore()
	handler := newImportDeliveriesHandler(store)

	request := importDeliveryRequest()
	request.SourceRecordID = "record-3"
	receiptState := "received_failed"
	receiptErrorCode := "invalid_number"
	receiptAt := fixedNow.Add(-30 * time.Minute)
	request.ReceiptState = &receiptState
	request.ReceiptErrorCode = &receiptErrorCode
	request.ReceiptAt = &receiptAt

	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	delivery := store.imported[0]
	if delivery.ReceiptState == nil || *delivery.ReceiptState != domain.ReceiptFailed {
		t.Fatalf("delivery.ReceiptState = %v, want %q", delivery.ReceiptState, domain.ReceiptFailed)
	}
	if delivery.ReceiptErrorCode == nil || *delivery.ReceiptErrorCode != "invalid_number" {
		t.Fatalf("delivery.ReceiptErrorCode = %v, want invalid_number", delivery.ReceiptErrorCode)
	}
	if delivery.ReceiptAt == nil || !delivery.ReceiptAt.Equal(fixedNow.Add(-30*time.Minute)) {
		t.Fatalf("delivery.ReceiptAt = %v", delivery.ReceiptAt)
	}
}

func TestImportDeliveriesDuplicateImportKeyIsErrDeliveryExists(t *testing.T) {
	store := newFakeDeliveryStore()
	handler := newImportDeliveriesHandler(store)

	if err := handler.Handle(context.Background(), importDeliveryRequest()); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	// Re-importing the same source record is idempotent on the import key.
	if err := handler.Handle(context.Background(), importDeliveryRequest()); !errors.Is(err, domain.ErrDeliveryExists) {
		t.Fatalf("second Handle() error = %v, want ErrDeliveryExists", err)
	}
	if len(store.imported) != 1 {
		t.Fatalf("imported deliveries = %d, want the duplicate to write nothing", len(store.imported))
	}
}

func TestImportDeliveriesRejectsInvalidRequestWithoutWriting(t *testing.T) {
	store := newFakeDeliveryStore()
	handler := newImportDeliveriesHandler(store)

	request := importDeliveryRequest()
	request.Channel = "push"
	if err := handler.Handle(context.Background(), request); !errors.Is(err, domain.ErrInvalidDeliveryChannel) {
		t.Fatalf("Handle() error = %v, want ErrInvalidDeliveryChannel", err)
	}
	if len(store.imported) != 0 {
		t.Fatalf("imported deliveries = %d, want none after a rejected request", len(store.imported))
	}
}

func TestImportDeliveriesPropagatesSaveError(t *testing.T) {
	store := newFakeDeliveryStore()
	store.saveImportedErr = errSaveFailed
	handler := newImportDeliveriesHandler(store)

	if err := handler.Handle(context.Background(), importDeliveryRequest()); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want errSaveFailed", err)
	}
}
