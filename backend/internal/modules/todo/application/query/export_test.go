package query

import (
	"context"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

func ptrTime(v time.Time) *time.Time { return &v }

// exportTodo seeds a historical todo in the given status directly into the
// fake store so each fixture carries a distinct created_at.
func exportTodo(t *testing.T, store *fakeTodoStore, id string, status domain.Status, createdAt time.Time) domain.Todo {
	t.Helper()
	var completedAt, deletedAt *time.Time
	version := 1
	switch status {
	case domain.StatusCompleted:
		completedAt = ptrTime(createdAt.Add(time.Hour))
		version = 2
	case domain.StatusDeleted:
		deletedAt = ptrTime(createdAt.Add(time.Hour))
		version = 2
	}
	todo, err := domain.Restore(id, "ws-1", "user-1", "历史"+id, nil, nil, nil,
		status, 1, version, createdAt, createdAt.Add(30*time.Minute), completedAt, deletedAt)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	store.todos[id] = todo
	return todo
}

func seedExportHistory(t *testing.T, store *fakeTodoStore) []domain.Todo {
	t.Helper()
	first := exportTodo(t, store, "todo-1", domain.StatusPending, fixedNow.Add(-3*time.Hour))
	second := exportTodo(t, store, "todo-2", domain.StatusCompleted, fixedNow.Add(-2*time.Hour))
	third := exportTodo(t, store, "todo-3", domain.StatusDeleted, fixedNow.Add(-time.Hour))
	return []domain.Todo{first, second, third}
}

func TestExportTodosMapsAllStatusesAndCapsLimit(t *testing.T) {
	store := newFakeStore()
	seeded := seedExportHistory(t, store)
	handler := &ExportTodosHandler{Store: store, Now: func() time.Time { return fixedNow }}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", 0, 500)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.listAllOffset != 0 || store.listAllLimit != dto.MaxListLimit {
		t.Fatalf("store paging = (%d, %d), want (0, %d)", store.listAllOffset, store.listAllLimit, dto.MaxListLimit)
	}
	if len(got) != 3 {
		t.Fatalf("records = %d, want 3 (deleted included)", len(got))
	}
	for index, record := range got {
		want := seeded[index]
		if record.ID != want.ID || record.Title != want.Title || record.Status != string(want.Status) {
			t.Fatalf("record[%d] = %#v, want aggregate %#v", index, record, want)
		}
		if record.ReminderVersion != want.ReminderVersion || !record.CreatedAt.Equal(want.CreatedAt) || !record.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatalf("record[%d] history = %#v", index, record)
		}
	}
	if got[1].CompletedAt == nil || got[2].DeletedAt == nil {
		t.Fatalf("terminal instants lost: %#v / %#v", got[1], got[2])
	}
	if got[0].CompletedAt != nil || got[0].DeletedAt != nil {
		t.Fatalf("pending record carries terminal instants: %#v", got[0])
	}
}

func TestExportTodosPagesWithOffsetAndLimit(t *testing.T) {
	store := newFakeStore()
	seedExportHistory(t, store)
	handler := &ExportTodosHandler{Store: store, Now: func() time.Time { return fixedNow }}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", 1, 1)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.listAllOffset != 1 || store.listAllLimit != 1 {
		t.Fatalf("store paging = (%d, %d), want (1, 1)", store.listAllOffset, store.listAllLimit)
	}
	if len(got) != 1 || got[0].ID != "todo-2" {
		t.Fatalf("page = %#v, want second record by created_at", got)
	}

	beyond, err := handler.Handle(context.Background(), "ws-1", "user-1", 10, 10)
	if err != nil {
		t.Fatalf("Handle(beyond) error = %v", err)
	}
	if len(beyond) != 0 {
		t.Fatalf("page beyond history = %#v, want empty", beyond)
	}
}
