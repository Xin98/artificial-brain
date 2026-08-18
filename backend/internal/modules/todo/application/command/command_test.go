package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

var fixedNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func ctx() context.Context { return context.Background() }

func newCreateHandler(store *fakeTodoStore, planner *fakePlanner, provider ports.ChannelsProvider) *CreateTodoHandler {
	return &CreateTodoHandler{
		Store:    store,
		UoW:      &fakeUoW{store: store},
		Planner:  planner,
		Channels: provider,
		NewID:    func() string { return "todo-new" },
		Now:      func() time.Time { return fixedNow },
	}
}

func dueAt(v time.Time) *time.Time { return &v }

func seedTodo(t *testing.T, store *fakeTodoStore, id string, due *time.Time) domain.Todo {
	t.Helper()
	todo, err := domain.New(id, "ws-1", "user-1", "提交周报", nil, due, nil, fixedNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := store.Insert(ctx(), todo); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	return todo
}

func TestCreateTodoWithoutDueNeverPlans(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	handler := newCreateHandler(store, planner, nil)

	got, err := handler.Handle(ctx(), dto.CreateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", Title: "无期任务",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.ID != "todo-new" || got.Status != string(domain.StatusPending) || got.Version != 1 {
		t.Fatalf("created dto = %#v", got)
	}
	if got.Overdue {
		t.Fatal("undated todo reported overdue")
	}
	if store.count() != 1 {
		t.Fatalf("stored todos = %d, want 1", store.count())
	}
	if len(planner.plans()) != 0 {
		t.Fatalf("plans = %#v, want none for undated todo", planner.plans())
	}
}

func TestCreateTodoWithDuePlansAtDueWithChannelSnapshot(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	provider := func(_ context.Context, workspaceID, ownerUserID string) ([]string, error) {
		if workspaceID != "ws-1" || ownerUserID != "user-1" {
			t.Fatalf("provider principal = %s/%s", workspaceID, ownerUserID)
		}
		return []string{"sms", "email"}, nil
	}
	handler := newCreateHandler(store, planner, provider)
	due := fixedNow.Add(2 * time.Hour)

	_, err := handler.Handle(ctx(), dto.CreateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", Title: "提交周报", DueAtUTC: &due,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	plans := planner.plans()
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	plan := plans[0]
	if plan.WorkspaceID != "ws-1" || plan.TodoID != "todo-new" || plan.TodoReminderVersion != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if !plan.ScheduledAtUTC.Equal(due) {
		t.Fatalf("plan.ScheduledAtUTC = %v, want due %v", plan.ScheduledAtUTC, due)
	}
	if len(plan.Channels) != 2 || plan.Channels[0] != "sms" || plan.Channels[1] != "email" {
		t.Fatalf("plan.Channels = %#v, want provider snapshot", plan.Channels)
	}
}

func TestCreateTodoWithNilChannelsProviderPlansEmptySnapshot(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	handler := newCreateHandler(store, planner, nil)
	due := fixedNow.Add(time.Hour)

	if _, err := handler.Handle(ctx(), dto.CreateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", Title: "提交周报", DueAtUTC: &due,
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	plans := planner.plans()
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1", len(plans))
	}
	if plans[0].Channels == nil || len(plans[0].Channels) != 0 {
		t.Fatalf("plan.Channels = %#v, want empty non-nil", plans[0].Channels)
	}
}

func TestCreateTodoPlannerFailureLeavesNoPartialResult(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	planner.planErr = errors.New("scheduler down")
	handler := newCreateHandler(store, planner, nil)
	due := fixedNow.Add(time.Hour)

	_, err := handler.Handle(ctx(), dto.CreateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", Title: "提交周报", DueAtUTC: &due,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want planner failure")
	}
	if store.count() != 0 {
		t.Fatalf("stored todos = %d, want 0 after rollback", store.count())
	}
}

func TestCreateTodoRejectsInvalidTitle(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	handler := newCreateHandler(store, planner, nil)

	_, err := handler.Handle(ctx(), dto.CreateTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", Title: ""})
	if !errors.Is(err, domain.ErrInvalidTitle) {
		t.Fatalf("Handle() error = %v, want ErrInvalidTitle", err)
	}
	if store.count() != 0 || len(planner.plans()) != 0 {
		t.Fatal("invalid create persisted state")
	}
}

func newLifecycleHandlers(store *fakeTodoStore, planner *fakePlanner) (*CompleteTodoHandler, *DeleteTodoHandler) {
	uow := &fakeUoW{store: store}
	now := func() time.Time { return fixedNow }
	return &CompleteTodoHandler{Store: store, UoW: uow, Planner: planner, Now: now},
		&DeleteTodoHandler{Store: store, UoW: uow, Planner: planner, Now: now}
}

func TestCompleteTodoRevokesAllPlanVersions(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", dueAt(fixedNow.Add(time.Hour)))
	complete, _ := newLifecycleHandlers(store, planner)

	got, err := complete.Handle(ctx(), dto.CompleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.Status != string(domain.StatusCompleted) || got.Version != 2 {
		t.Fatalf("completed dto = %#v", got)
	}
	revokes := planner.revokes()
	if len(revokes) != 1 {
		t.Fatalf("revokes = %d, want 1", len(revokes))
	}
	if revokes[0].WorkspaceID != "ws-1" || revokes[0].TodoID != "todo-1" || revokes[0].UpToReminderVersion != 1 {
		t.Fatalf("revoke = %#v", revokes[0])
	}
}

func TestCompleteTodoStaleVersionConflicts(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", nil)
	complete, _ := newLifecycleHandlers(store, planner)

	_, err := complete.Handle(ctx(), dto.CompleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 99})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Handle() error = %v, want ErrConflict", err)
	}
	if len(planner.revokes()) != 0 {
		t.Fatal("conflicted complete revoked plans")
	}
}

func TestCompleteTodoNotFound(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	complete, _ := newLifecycleHandlers(store, planner)

	_, err := complete.Handle(ctx(), dto.CompleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "missing", Version: 1})
	if !errors.Is(err, domain.ErrTodoNotFound) {
		t.Fatalf("Handle() error = %v, want ErrTodoNotFound", err)
	}
}

func TestCompleteTodoRevokeFailureRollsBack(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	planner.revokeErr = errors.New("planner down")
	seedTodo(t, store, "todo-1", dueAt(fixedNow))
	complete, _ := newLifecycleHandlers(store, planner)

	_, err := complete.Handle(ctx(), dto.CompleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1})
	if err == nil {
		t.Fatal("Handle() error = nil, want revoke failure")
	}
	stored, getErr := store.Get(ctx(), "ws-1", "user-1", "todo-1")
	if getErr != nil || stored.Status != domain.StatusPending {
		t.Fatalf("stored todo after rollback = %#v, err = %v", stored, getErr)
	}
}

func TestDeleteTodoRevokesPlansAndReportsDeleted(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", dueAt(fixedNow.Add(time.Hour)))
	_, del := newLifecycleHandlers(store, planner)

	got, err := del.Handle(ctx(), dto.DeleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.Status != string(domain.StatusDeleted) || got.Version != 2 {
		t.Fatalf("deleted dto = %#v", got)
	}
	revokes := planner.revokes()
	if len(revokes) != 1 || revokes[0].UpToReminderVersion != 1 {
		t.Fatalf("revokes = %#v", revokes)
	}
}

func TestDeleteTodoNotFoundAndConflict(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", nil)
	_, del := newLifecycleHandlers(store, planner)

	if _, err := del.Handle(ctx(), dto.DeleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "missing", Version: 1}); !errors.Is(err, domain.ErrTodoNotFound) {
		t.Fatalf("Handle(missing) error = %v, want ErrTodoNotFound", err)
	}
	if _, err := del.Handle(ctx(), dto.DeleteTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 99}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Handle(stale) error = %v, want ErrConflict", err)
	}
}

func newUpdateHandler(store *fakeTodoStore, planner *fakePlanner, provider ports.ChannelsProvider) *UpdateTodoHandler {
	return &UpdateTodoHandler{
		Store:    store,
		UoW:      &fakeUoW{store: store},
		Planner:  planner,
		Channels: provider,
		Now:      func() time.Time { return fixedNow },
	}
}

func TestUpdateTitleOnlyNeverTouchesPlanner(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", dueAt(fixedNow.Add(time.Hour)))
	handler := newUpdateHandler(store, planner, nil)
	title := "新标题"

	got, err := handler.Handle(ctx(), dto.UpdateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1, Title: &title,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.Title != title || got.Version != 2 || got.ReminderVersion != 1 {
		t.Fatalf("updated dto = %#v", got)
	}
	if len(planner.calls) != 0 {
		t.Fatalf("planner calls = %#v, want none", planner.calls)
	}
}

func TestUpdateDueReschedulesWithRevokeThenPlan(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", dueAt(fixedNow.Add(time.Hour)))
	handler := newUpdateHandler(store, planner, func(context.Context, string, string) ([]string, error) {
		return []string{"email"}, nil
	})
	newDue := fixedNow.Add(48 * time.Hour)

	got, err := handler.Handle(ctx(), dto.UpdateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1,
		DueChanged: true, DueAtUTC: &newDue,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.ReminderVersion != 2 {
		t.Fatalf("dto.ReminderVersion = %d, want 2", got.ReminderVersion)
	}
	if len(planner.calls) != 2 || planner.calls[0].revoke == nil || planner.calls[1].plan == nil {
		t.Fatalf("planner calls = %#v, want revoke then plan", planner.calls)
	}
	revoke, plan := *planner.calls[0].revoke, *planner.calls[1].plan
	if revoke.TodoID != "todo-1" || revoke.UpToReminderVersion != 1 {
		t.Fatalf("revoke = %#v", revoke)
	}
	if plan.TodoID != "todo-1" || plan.TodoReminderVersion != 2 || !plan.ScheduledAtUTC.Equal(newDue) {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Channels) != 1 || plan.Channels[0] != "email" {
		t.Fatalf("plan.Channels = %#v, want provider snapshot", plan.Channels)
	}
}

func TestUpdateClearDueOnlyRevokes(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", dueAt(fixedNow.Add(time.Hour)))
	handler := newUpdateHandler(store, planner, nil)

	got, err := handler.Handle(ctx(), dto.UpdateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1,
		DueChanged: true, DueAtUTC: nil,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.ReminderVersion != 2 || got.DueAtUTC != nil {
		t.Fatalf("updated dto = %#v", got)
	}
	if len(planner.revokes()) != 1 || len(planner.plans()) != 0 {
		t.Fatalf("planner calls = %#v, want revoke only", planner.calls)
	}
}

func TestUpdateStaleVersionConflictsWithoutPlanner(t *testing.T) {
	store := newFakeTodoStore()
	planner := newFakePlanner()
	seedTodo(t, store, "todo-1", dueAt(fixedNow.Add(time.Hour)))
	handler := newUpdateHandler(store, planner, nil)
	newDue := fixedNow.Add(48 * time.Hour)

	_, err := handler.Handle(ctx(), dto.UpdateTodoRequest{
		WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 99,
		DueChanged: true, DueAtUTC: &newDue,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Handle() error = %v, want ErrConflict", err)
	}
	if len(planner.calls) != 0 {
		t.Fatalf("planner calls = %#v, want none on conflict", planner.calls)
	}
}
