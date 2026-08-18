package domain

import (
	"errors"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func TestNewConfirmationRequestBindsAllDimensions(t *testing.T) {
	confirmation, err := NewConfirmationRequest("conf-1", "ws-1", "user-1", IntentTodoDelete, "todo-1", 3, testNow, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewConfirmationRequest() error = %v", err)
	}
	if confirmation.ID != "conf-1" || confirmation.WorkspaceID != "ws-1" || confirmation.UserID != "user-1" {
		t.Fatalf("confirmation identity = %#v", confirmation)
	}
	if confirmation.Intent != IntentTodoDelete || confirmation.TodoID != "todo-1" || confirmation.TodoVersion != 3 {
		t.Fatalf("confirmation binding = %#v", confirmation)
	}
	if !confirmation.CreatedAt.Equal(testNow) || !confirmation.ExpiresAt.Equal(testNow.Add(5*time.Minute)) {
		t.Fatalf("confirmation window = %v..%v", confirmation.CreatedAt, confirmation.ExpiresAt)
	}
	if confirmation.ConsumedAt != nil {
		t.Fatalf("confirmation.ConsumedAt = %v, want nil", confirmation.ConsumedAt)
	}
}

func TestNewConfirmationRequestOnlySupportsDelete(t *testing.T) {
	if _, err := NewConfirmationRequest("conf-1", "ws-1", "user-1", IntentTodoCreate, "todo-1", 1, testNow, time.Minute); !errors.Is(err, ErrUnsupportedConfirmationIntent) {
		t.Fatalf("NewConfirmationRequest(create) error = %v, want ErrUnsupportedConfirmationIntent", err)
	}
}

func TestConfirmationConsumeIsSingleUse(t *testing.T) {
	confirmation, err := NewConfirmationRequest("conf-1", "ws-1", "user-1", IntentTodoDelete, "todo-1", 1, testNow, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewConfirmationRequest() error = %v", err)
	}
	if confirmation.IsConsumed() {
		t.Fatal("new confirmation reports consumed")
	}
	if err := confirmation.Consume(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if !confirmation.IsConsumed() || confirmation.ConsumedAt == nil || !confirmation.ConsumedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("confirmation after consume = %#v", confirmation)
	}
	if err := confirmation.Consume(testNow.Add(2 * time.Minute)); !errors.Is(err, ErrConfirmationConsumed) {
		t.Fatalf("second Consume() error = %v, want ErrConfirmationConsumed", err)
	}
	if !confirmation.ConsumedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("second Consume changed ConsumedAt to %v", confirmation.ConsumedAt)
	}
}

func TestConfirmationExpiryBoundary(t *testing.T) {
	confirmation, err := NewConfirmationRequest("conf-1", "ws-1", "user-1", IntentTodoDelete, "todo-1", 1, testNow, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewConfirmationRequest() error = %v", err)
	}
	if confirmation.IsExpired(testNow.Add(5 * time.Minute).Add(-time.Nanosecond)) {
		t.Fatal("confirmation expired one nanosecond before its deadline")
	}
	if !confirmation.IsExpired(testNow.Add(5 * time.Minute)) {
		t.Fatal("confirmation not expired exactly at its deadline")
	}
	if err := confirmation.Consume(testNow.Add(5 * time.Minute)); !errors.Is(err, ErrConfirmationExpired) {
		t.Fatalf("Consume(at deadline) error = %v, want ErrConfirmationExpired", err)
	}
	if err := confirmation.Consume(testNow.Add(6 * time.Minute)); !errors.Is(err, ErrConfirmationExpired) {
		t.Fatalf("Consume(after deadline) error = %v, want ErrConfirmationExpired", err)
	}
	if confirmation.IsConsumed() {
		t.Fatal("expired confirmation reported consumed")
	}
}

func TestClarificationCarriesMissingFieldsAndReason(t *testing.T) {
	clarification := Clarification{MissingFields: []string{"title"}, Reason: ReasonMissingFields}
	if len(clarification.MissingFields) != 1 || clarification.MissingFields[0] != "title" || clarification.Reason != ReasonMissingFields {
		t.Fatalf("clarification = %#v", clarification)
	}
}
