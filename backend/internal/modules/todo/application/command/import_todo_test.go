package command

import (
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

func newImportHandler(store *fakeTodoStore) *ImportTodoHandler {
	return &ImportTodoHandler{
		Store: store,
		NewID: func() string { return "todo-imported" },
		Now:   func() time.Time { return fixedNow },
	}
}

func importRequest(status string) dto.ImportTodoRequest {
	return dto.ImportTodoRequest{
		WorkspaceID:     "ws-1",
		UserID:          "user-1",
		Title:           "迁移任务",
		Status:          status,
		ReminderVersion: 4,
		Version:         7,
		CreatedAt:       fixedNow.Add(-72 * time.Hour),
		UpdatedAt:       fixedNow.Add(-48 * time.Hour),
	}
}

func TestImportTodoRestoresAggregateWithoutPlanning(t *testing.T) {
	store := newFakeTodoStore()
	handler := newImportHandler(store)
	due := fixedNow.Add(-time.Hour)
	description := "迁移来的说明"
	request := importRequest(string(domain.StatusPending))
	request.Description = &description
	request.DueAtUTC = &due

	got, err := handler.Handle(ctx(), request)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.ID != "todo-imported" || got.Status != string(domain.StatusPending) {
		t.Fatalf("imported dto = %#v", got)
	}
	if got.ReminderVersion != 4 || got.Version != 7 {
		t.Fatalf("imported versions = %d/%d, want historical 4/7", got.ReminderVersion, got.Version)
	}
	if !got.CreatedAt.Equal(request.CreatedAt) || !got.UpdatedAt.Equal(request.UpdatedAt) {
		t.Fatalf("imported timestamps = %v/%v, want historical", got.CreatedAt, got.UpdatedAt)
	}
	// The due is already past, so the derived overdue flag reflects now.
	if !got.Overdue {
		t.Fatal("imported past-due todo not reported overdue")
	}

	stored, err := store.Get(ctx(), "ws-1", "user-1", "todo-imported")
	if err != nil {
		t.Fatalf("Get(imported) error = %v", err)
	}
	if stored.Title != "迁移任务" || stored.Description == nil || *stored.Description != description {
		t.Fatalf("stored aggregate = %#v", stored)
	}
	if stored.DueAtUTC == nil || !stored.DueAtUTC.Equal(due) {
		t.Fatalf("stored due = %v, want original %v", stored.DueAtUTC, due)
	}
	if stored.ReminderVersion != 4 || stored.Version != 7 {
		t.Fatalf("stored versions = %d/%d, want original 4/7", stored.ReminderVersion, stored.Version)
	}
	if !stored.CreatedAt.Equal(request.CreatedAt) || !stored.UpdatedAt.Equal(request.UpdatedAt) {
		t.Fatalf("stored timestamps = %v/%v, want original historical", stored.CreatedAt, stored.UpdatedAt)
	}
}

func TestImportTodoCompletedAndDeletedCarryInstants(t *testing.T) {
	store := newFakeTodoStore()
	completedAt := fixedNow.Add(-24 * time.Hour)
	completed := importRequest(string(domain.StatusCompleted))
	completed.CompletedAt = &completedAt
	if _, err := newImportHandler(store).Handle(ctx(), completed); err != nil {
		t.Fatalf("Handle(completed) error = %v", err)
	}
	stored, err := store.Get(ctx(), "ws-1", "user-1", "todo-imported")
	if err != nil {
		t.Fatalf("Get(completed) error = %v", err)
	}
	if stored.Status != domain.StatusCompleted || stored.CompletedAt == nil || !stored.CompletedAt.Equal(completedAt) {
		t.Fatalf("stored completed = %#v", stored)
	}

	deletedStore := newFakeTodoStore()
	deletedAt := fixedNow.Add(-12 * time.Hour)
	deleted := importRequest(string(domain.StatusDeleted))
	deleted.DeletedAt = &deletedAt
	if _, err := newImportHandler(deletedStore).Handle(ctx(), deleted); err != nil {
		t.Fatalf("Handle(deleted) error = %v", err)
	}
	stored, err = deletedStore.Get(ctx(), "ws-1", "user-1", "todo-imported")
	if err != nil {
		t.Fatalf("Get(deleted) error = %v", err)
	}
	if stored.Status != domain.StatusDeleted || stored.DeletedAt == nil || !stored.DeletedAt.Equal(deletedAt) {
		t.Fatalf("stored deleted = %#v", stored)
	}
}

func TestImportTodoRejectsInvalidRequest(t *testing.T) {
	cases := map[string]func(dto.ImportTodoRequest) dto.ImportTodoRequest{
		"empty workspace": func(r dto.ImportTodoRequest) dto.ImportTodoRequest { r.WorkspaceID = ""; return r },
		"empty user":      func(r dto.ImportTodoRequest) dto.ImportTodoRequest { r.UserID = ""; return r },
	}
	for name, mutate := range cases {
		store := newFakeTodoStore()
		_, err := newImportHandler(store).Handle(ctx(), mutate(importRequest(string(domain.StatusPending))))
		if !errors.Is(err, domain.ErrMissingRestoreFields) {
			t.Fatalf("Handle(%s) error = %v, want ErrMissingRestoreFields", name, err)
		}
		if store.count() != 0 {
			t.Fatalf("Handle(%s) stored %d todos, want none", name, store.count())
		}
	}

	store := newFakeTodoStore()
	_, err := newImportHandler(store).Handle(ctx(), importRequest("archived"))
	if !errors.Is(err, domain.ErrInvalidStatus) {
		t.Fatalf("Handle(unknown status) error = %v, want ErrInvalidStatus", err)
	}
	blank := importRequest(string(domain.StatusPending))
	blank.Title = ""
	if _, err := newImportHandler(store).Handle(ctx(), blank); !errors.Is(err, domain.ErrInvalidTitle) {
		t.Fatalf("Handle(empty title) error = %v, want ErrInvalidTitle", err)
	}
	if store.count() != 0 {
		t.Fatalf("invalid imports stored %d todos, want none", store.count())
	}
}
