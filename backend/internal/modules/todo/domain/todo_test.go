package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func dueAt(v time.Time) *time.Time { return &v }

func newPendingTodo(t *testing.T) Todo {
	t.Helper()
	todo, err := New("todo-1", "ws-1", "user-1", "写周报", nil, dueAt(testNow.Add(time.Hour)), nil, testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return todo
}

func TestNewTodoDefaults(t *testing.T) {
	todo := newPendingTodo(t)
	if todo.ID != "todo-1" || todo.WorkspaceID != "ws-1" || todo.OwnerUserID != "user-1" {
		t.Fatalf("todo identity = %#v", todo)
	}
	if todo.Status != StatusPending {
		t.Fatalf("todo.Status = %q, want %q", todo.Status, StatusPending)
	}
	if todo.Version != 1 || todo.ReminderVersion != 1 {
		t.Fatalf("versions = %d/%d, want 1/1", todo.Version, todo.ReminderVersion)
	}
	if !todo.CreatedAt.Equal(testNow) || !todo.UpdatedAt.Equal(testNow) {
		t.Fatalf("timestamps = %v/%v", todo.CreatedAt, todo.UpdatedAt)
	}
	if todo.CompletedAt != nil || todo.DeletedAt != nil {
		t.Fatalf("unexpected terminal timestamps: %#v", todo)
	}
}

func TestNewTodoAllowsOptionalDueAndDescription(t *testing.T) {
	todo, err := New("todo-1", "ws-1", "user-1", "无期任务", nil, nil, nil, testNow)
	if err != nil {
		t.Fatalf("New(no due) error = %v", err)
	}
	if todo.DueAtUTC != nil || todo.Description != nil {
		t.Fatalf("todo = %#v, want nil due and description", todo)
	}
}

func TestNewTodoEnforcesTitleBounds(t *testing.T) {
	if _, err := New("todo-1", "ws-1", "user-1", "", nil, nil, nil, testNow); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("New(empty title) error = %v, want ErrInvalidTitle", err)
	}
	if _, err := New("todo-1", "ws-1", "user-1", strings.Repeat("a", 200), nil, nil, nil, testNow); err != nil {
		t.Fatalf("New(200 chars) error = %v", err)
	}
	if _, err := New("todo-1", "ws-1", "user-1", strings.Repeat("a", 201), nil, nil, nil, testNow); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("New(201 chars) error = %v, want ErrInvalidTitle", err)
	}
	// Bounds are rune-based: 200 CJK characters fit.
	if _, err := New("todo-1", "ws-1", "user-1", strings.Repeat("周", 200), nil, nil, nil, testNow); err != nil {
		t.Fatalf("New(200 CJK) error = %v", err)
	}
}

func TestCompleteTransitionsPendingTodo(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Complete(1, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if todo.Status != StatusCompleted || todo.CompletedAt == nil || !todo.CompletedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("todo after Complete = %#v", todo)
	}
	if todo.Version != 2 {
		t.Fatalf("todo.Version = %d, want 2", todo.Version)
	}
}

func TestCompleteRejectsStaleVersion(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Complete(99, testNow); !errors.Is(err, ErrConflict) {
		t.Fatalf("Complete(stale) error = %v, want ErrConflict", err)
	}
}

func TestCompleteRejectsDeletedTodo(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Delete(1, testNow); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := todo.Complete(2, testNow); !errors.Is(err, ErrTodoDeleted) {
		t.Fatalf("Complete(deleted) error = %v, want ErrTodoDeleted", err)
	}
}

func TestCompleteRejectsCompletedTodo(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Complete(1, testNow); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := todo.Complete(2, testNow); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("second Complete() error = %v, want ErrAlreadyCompleted", err)
	}
}

func TestDeleteIsSoftAndTerminal(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Delete(1, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if todo.Status != StatusDeleted || todo.DeletedAt == nil || !todo.DeletedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("todo after Delete = %#v", todo)
	}
	if todo.Version != 2 {
		t.Fatalf("todo.Version = %d, want 2", todo.Version)
	}
	if err := todo.Delete(2, testNow); !errors.Is(err, ErrTodoDeleted) {
		t.Fatalf("second Delete() error = %v, want ErrTodoDeleted", err)
	}
}

func TestDeleteRejectsStaleVersion(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Delete(99, testNow); !errors.Is(err, ErrConflict) {
		t.Fatalf("Delete(stale) error = %v, want ErrConflict", err)
	}
}

func TestDeleteAllowsCompletedTodo(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Complete(1, testNow); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := todo.Delete(2, testNow); err != nil {
		t.Fatalf("Delete(completed) error = %v", err)
	}
}

func TestUpdateEditsFieldsAndBumpsVersion(t *testing.T) {
	todo := newPendingTodo(t)
	title := "改标题"
	description := "补充说明"
	err := todo.Update(1, UpdateChanges{Title: &title, Description: &description}, testNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if todo.Title != title || todo.Description == nil || *todo.Description != description {
		t.Fatalf("todo after Update = %#v", todo)
	}
	if todo.Version != 2 || !todo.UpdatedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("version/updated = %d/%v", todo.Version, todo.UpdatedAt)
	}
	if todo.ReminderVersion != 1 {
		t.Fatalf("ReminderVersion = %d, want unchanged 1", todo.ReminderVersion)
	}
}

func TestUpdateRejectsStaleVersionAndDeleted(t *testing.T) {
	todo := newPendingTodo(t)
	title := "x"
	if err := todo.Update(99, UpdateChanges{Title: &title}, testNow); !errors.Is(err, ErrConflict) {
		t.Fatalf("Update(stale) error = %v, want ErrConflict", err)
	}
	if err := todo.Delete(1, testNow); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := todo.Update(2, UpdateChanges{Title: &title}, testNow); !errors.Is(err, ErrTodoDeleted) {
		t.Fatalf("Update(deleted) error = %v, want ErrTodoDeleted", err)
	}
}

func TestUpdateEnforcesTitleBounds(t *testing.T) {
	todo := newPendingTodo(t)
	title := strings.Repeat("a", 201)
	if err := todo.Update(1, UpdateChanges{Title: &title}, testNow); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("Update(long title) error = %v, want ErrInvalidTitle", err)
	}
}

func TestUpdateDueChangeBumpsReminderVersion(t *testing.T) {
	todo := newPendingTodo(t)
	newDue := testNow.Add(48 * time.Hour)
	if err := todo.Update(1, UpdateChanges{DueChanged: true, DueAtUTC: &newDue}, testNow); err != nil {
		t.Fatalf("Update(reschedule) error = %v", err)
	}
	if todo.ReminderVersion != 2 {
		t.Fatalf("ReminderVersion = %d, want 2", todo.ReminderVersion)
	}
	if todo.DueAtUTC == nil || !todo.DueAtUTC.Equal(newDue) {
		t.Fatalf("DueAtUTC = %v, want %v", todo.DueAtUTC, newDue)
	}
}

func TestUpdateClearingDueBumpsReminderVersion(t *testing.T) {
	todo := newPendingTodo(t)
	if err := todo.Update(1, UpdateChanges{DueChanged: true, DueAtUTC: nil}, testNow); err != nil {
		t.Fatalf("Update(clear due) error = %v", err)
	}
	if todo.DueAtUTC != nil {
		t.Fatalf("DueAtUTC = %v, want nil", todo.DueAtUTC)
	}
	if todo.ReminderVersion != 2 {
		t.Fatalf("ReminderVersion = %d, want 2", todo.ReminderVersion)
	}
}

func TestUpdateSameDueDoesNotBumpReminderVersion(t *testing.T) {
	todo := newPendingTodo(t)
	sameDue := testNow.Add(time.Hour)
	if err := todo.Update(1, UpdateChanges{DueChanged: true, DueAtUTC: &sameDue}, testNow); err != nil {
		t.Fatalf("Update(same due) error = %v", err)
	}
	if todo.ReminderVersion != 1 {
		t.Fatalf("ReminderVersion = %d, want unchanged 1", todo.ReminderVersion)
	}
	if todo.Version != 2 {
		t.Fatalf("Version = %d, want bumped 2", todo.Version)
	}
}

func TestRestoreRebuildsEachStatusWithHistoricalState(t *testing.T) {
	description := "迁移来的说明"
	due := testNow.Add(-24 * time.Hour)
	timezone := "Asia/Shanghai"
	createdAt := testNow.Add(-72 * time.Hour)
	updatedAt := testNow.Add(-48 * time.Hour)

	pending, err := Restore("todo-old", "ws-old", "user-old", "历史任务", &description,
		&due, &timezone, StatusPending, 4, 7, createdAt, updatedAt, nil, nil)
	if err != nil {
		t.Fatalf("Restore(pending) error = %v", err)
	}
	if pending.ID != "todo-old" || pending.WorkspaceID != "ws-old" || pending.OwnerUserID != "user-old" {
		t.Fatalf("restored identity = %#v", pending)
	}
	if pending.Status != StatusPending || pending.ReminderVersion != 4 || pending.Version != 7 {
		t.Fatalf("restored status/versions = %q/%d/%d", pending.Status, pending.ReminderVersion, pending.Version)
	}
	if !pending.CreatedAt.Equal(createdAt) || !pending.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("restored timestamps = %v/%v, want historical", pending.CreatedAt, pending.UpdatedAt)
	}
	if pending.DueAtUTC == nil || !pending.DueAtUTC.Equal(due) || pending.Description == nil || *pending.Description != description {
		t.Fatalf("restored due/description = %#v", pending)
	}
	if pending.TimezoneAtInput == nil || *pending.TimezoneAtInput != timezone {
		t.Fatalf("restored timezone = %#v", pending.TimezoneAtInput)
	}
	if pending.CompletedAt != nil || pending.DeletedAt != nil {
		t.Fatalf("pending restore carries terminal timestamps: %#v", pending)
	}

	completedAt := testNow.Add(-30 * time.Hour)
	completed, err := Restore("todo-done", "ws-old", "user-old", "已完成任务", nil,
		nil, nil, StatusCompleted, 1, 2, createdAt, updatedAt, &completedAt, nil)
	if err != nil {
		t.Fatalf("Restore(completed) error = %v", err)
	}
	if completed.Status != StatusCompleted || completed.CompletedAt == nil || !completed.CompletedAt.Equal(completedAt) {
		t.Fatalf("restored completed = %#v", completed)
	}

	deletedAt := testNow.Add(-12 * time.Hour)
	deleted, err := Restore("todo-gone", "ws-old", "user-old", "已删除任务", nil,
		nil, nil, StatusDeleted, 1, 3, createdAt, updatedAt, nil, &deletedAt)
	if err != nil {
		t.Fatalf("Restore(deleted) error = %v", err)
	}
	if deleted.Status != StatusDeleted || deleted.DeletedAt == nil || !deleted.DeletedAt.Equal(deletedAt) {
		t.Fatalf("restored deleted = %#v", deleted)
	}
}

func TestRestoreRejectsInvalidTitleAndStatus(t *testing.T) {
	createdAt := testNow.Add(-time.Hour)
	if _, err := Restore("todo-1", "ws-1", "user-1", "", nil, nil, nil,
		StatusPending, 1, 1, createdAt, createdAt, nil, nil); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("Restore(empty title) error = %v, want ErrInvalidTitle", err)
	}
	if _, err := Restore("todo-1", "ws-1", "user-1", strings.Repeat("a", 201), nil, nil, nil,
		StatusPending, 1, 1, createdAt, createdAt, nil, nil); !errors.Is(err, ErrInvalidTitle) {
		t.Fatalf("Restore(long title) error = %v, want ErrInvalidTitle", err)
	}
	for _, status := range []Status{"", "archived", "done"} {
		if _, err := Restore("todo-1", "ws-1", "user-1", "历史任务", nil, nil, nil,
			status, 1, 1, createdAt, createdAt, nil, nil); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("Restore(%q) error = %v, want ErrInvalidStatus", status, err)
		}
	}
}

func TestRestoreEnforcesStatusTimestampInvariants(t *testing.T) {
	createdAt := testNow.Add(-time.Hour)
	if _, err := Restore("todo-1", "ws-1", "user-1", "历史任务", nil, nil, nil,
		StatusCompleted, 1, 2, createdAt, createdAt, nil, nil); !errors.Is(err, ErrInconsistentStatus) {
		t.Fatalf("Restore(completed without completedAt) error = %v, want ErrInconsistentStatus", err)
	}
	if _, err := Restore("todo-1", "ws-1", "user-1", "历史任务", nil, nil, nil,
		StatusDeleted, 1, 2, createdAt, createdAt, nil, nil); !errors.Is(err, ErrInconsistentStatus) {
		t.Fatalf("Restore(deleted without deletedAt) error = %v, want ErrInconsistentStatus", err)
	}
}

func TestRestoreRejectsEmptyIdentity(t *testing.T) {
	createdAt := testNow.Add(-time.Hour)
	if _, err := Restore("", "ws-1", "user-1", "历史任务", nil, nil, nil,
		StatusPending, 1, 1, createdAt, createdAt, nil, nil); !errors.Is(err, ErrMissingRestoreFields) {
		t.Fatalf("Restore(empty id) error = %v, want ErrMissingRestoreFields", err)
	}
	if _, err := Restore("todo-1", "", "user-1", "历史任务", nil, nil, nil,
		StatusPending, 1, 1, createdAt, createdAt, nil, nil); !errors.Is(err, ErrMissingRestoreFields) {
		t.Fatalf("Restore(empty workspace) error = %v, want ErrMissingRestoreFields", err)
	}
	if _, err := Restore("todo-1", "ws-1", "", "历史任务", nil, nil, nil,
		StatusPending, 1, 1, createdAt, createdAt, nil, nil); !errors.Is(err, ErrMissingRestoreFields) {
		t.Fatalf("Restore(empty owner) error = %v, want ErrMissingRestoreFields", err)
	}
}

func TestIsOverdueIsDerived(t *testing.T) {
	todo := newPendingTodo(t) // due one hour after testNow
	if todo.IsOverdue(testNow) {
		t.Fatal("future due reported overdue")
	}
	if !todo.IsOverdue(testNow.Add(2 * time.Hour)) {
		t.Fatal("past due not reported overdue")
	}
	undated, err := New("todo-2", "ws-1", "user-1", "无期", nil, nil, nil, testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if undated.IsOverdue(testNow.Add(24 * time.Hour)) {
		t.Fatal("undated todo reported overdue")
	}
	if err := todo.Complete(1, testNow); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if todo.IsOverdue(testNow.Add(2 * time.Hour)) {
		t.Fatal("completed todo reported overdue")
	}
}
