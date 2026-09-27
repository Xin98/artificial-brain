package domain

import (
	"errors"
	"strings"
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

func TestNewSessionValidatesIdentifiersAndTitle(t *testing.T) {
	session, err := NewSession("s-1", "ws-1", "user-1", "  周报会话  ", testNow)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if session.ID != "s-1" || session.WorkspaceID != "ws-1" || session.UserID != "user-1" {
		t.Fatalf("session identity = %#v", session)
	}
	if session.Title != "周报会话" {
		t.Fatalf("session.Title = %q, want trimmed", session.Title)
	}
	if !session.CreatedAt.Equal(testNow) || !session.UpdatedAt.Equal(testNow) {
		t.Fatalf("session window = %v..%v", session.CreatedAt, session.UpdatedAt)
	}

	for _, title := range []string{"", "   ", strings.Repeat("长", MaxSessionTitleRunes+1)} {
		if _, err := NewSession("s-1", "ws-1", "user-1", title, testNow); !errors.Is(err, ErrSessionTitleInvalid) {
			t.Fatalf("NewSession(title=%q) error = %v, want ErrSessionTitleInvalid", title, err)
		}
	}
	for _, id := range []string{"", " "} {
		if _, err := NewSession(id, "ws-1", "user-1", "标题", testNow); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("NewSession(id=%q) error = %v, want ErrInvalidSession", id, err)
		}
	}
	if _, err := NewSession("s-1", "", "user-1", "标题", testNow); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("NewSession(no workspace) error = %v, want ErrInvalidSession", err)
	}
	if _, err := NewSession("s-1", "ws-1", " ", "标题", testNow); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("NewSession(no user) error = %v, want ErrInvalidSession", err)
	}
}

func TestSessionRenamedValidatesAndTouches(t *testing.T) {
	session, err := NewSession("s-1", "ws-1", "user-1", "旧标题", testNow)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	renamed, err := session.Renamed(" 新标题 ", testNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("Renamed() error = %v", err)
	}
	if renamed.Title != "新标题" || !renamed.UpdatedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("renamed = %#v", renamed)
	}
	if !renamed.CreatedAt.Equal(testNow) || renamed.ID != session.ID {
		t.Fatalf("Renamed changed identity: %#v", renamed)
	}
	if session.Title != "旧标题" {
		t.Fatalf("Renamed mutated the receiver: %#v", session)
	}
	if _, err := session.Renamed(strings.Repeat("长", MaxSessionTitleRunes+1), testNow); !errors.Is(err, ErrSessionTitleInvalid) {
		t.Fatalf("Renamed(too long) error = %v, want ErrSessionTitleInvalid", err)
	}
	if _, err := session.Renamed("  ", testNow); !errors.Is(err, ErrSessionTitleInvalid) {
		t.Fatalf("Renamed(blank) error = %v, want ErrSessionTitleInvalid", err)
	}
}

func TestDefaultSessionTitle(t *testing.T) {
	if got := DefaultSessionTitle("明天下午三点提醒我提交周报"); got != "明天下午三点提醒我提交周报" {
		t.Fatalf("DefaultSessionTitle(short) = %q", got)
	}
	long := strings.Repeat("字", DefaultSessionTitleRunes+10)
	if got := DefaultSessionTitle(long); got != strings.Repeat("字", DefaultSessionTitleRunes) {
		t.Fatalf("DefaultSessionTitle(long) truncated to %d runes, want %d", len([]rune(got)), DefaultSessionTitleRunes)
	}
	if got := DefaultSessionTitle("   \n\t "); got != DefaultSessionTitleFallback {
		t.Fatalf("DefaultSessionTitle(blank) = %q, want fallback", got)
	}
	if got := DefaultSessionTitle("  提醒我开会  "); got != "提醒我开会" {
		t.Fatalf("DefaultSessionTitle(padded) = %q, want trimmed", got)
	}
}
