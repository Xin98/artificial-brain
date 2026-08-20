package query

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

type dashboardArgs struct {
	now           time.Time
	dueTodayStart time.Time
	dueTodayEnd   time.Time
}

type fakeTodoStore struct {
	todos          map[string]domain.Todo
	listFilters    dto.ListFilters
	listLimit      int
	dashboard      dashboardArgs
	dashboardReply dto.DashboardSummary
	candidateLimit int
	candidateReply []dto.Candidate
}

func newFakeStore() *fakeTodoStore { return &fakeTodoStore{todos: map[string]domain.Todo{}} }

func (s *fakeTodoStore) Insert(_ context.Context, todo domain.Todo) error {
	s.todos[todo.ID] = todo
	return nil
}

func (s *fakeTodoStore) Get(_ context.Context, workspaceID, ownerUserID, todoID string) (domain.Todo, error) {
	todo, ok := s.todos[todoID]
	if !ok || todo.WorkspaceID != workspaceID || todo.OwnerUserID != ownerUserID {
		return domain.Todo{}, domain.ErrTodoNotFound
	}
	return todo, nil
}

func (s *fakeTodoStore) Update(_ context.Context, todo domain.Todo, expectedVersion int) error {
	s.todos[todo.ID] = todo
	return nil
}

func (s *fakeTodoStore) List(_ context.Context, _, _ string, filters dto.ListFilters, limit int) ([]domain.Todo, error) {
	s.listFilters = filters
	s.listLimit = limit
	var result []domain.Todo
	for _, todo := range s.todos {
		if todo.Status != domain.StatusDeleted {
			result = append(result, todo)
		}
	}
	return result, nil
}

func (s *fakeTodoStore) Dashboard(_ context.Context, _, _ string, now, dueTodayStart, dueTodayEnd time.Time) (dto.DashboardSummary, error) {
	s.dashboard = dashboardArgs{now, dueTodayStart, dueTodayEnd}
	return s.dashboardReply, nil
}

func (s *fakeTodoStore) SearchCandidates(_ context.Context, _, _, _ string, limit int) ([]dto.Candidate, error) {
	s.candidateLimit = limit
	return s.candidateReply, nil
}

func seedTodo(t *testing.T, store *fakeTodoStore, id string, due *time.Time) {
	t.Helper()
	todo, err := domain.New(id, "ws-1", "user-1", "提交周报", nil, due, nil, fixedNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	store.todos[id] = todo
}

func TestListTodosCapsLimitAndMapsOverdue(t *testing.T) {
	store := newFakeStore()
	overdue := fixedNow.Add(-time.Hour)
	future := fixedNow.Add(time.Hour)
	seedTodo(t, store, "todo-overdue", &overdue)
	seedTodo(t, store, "todo-future", &future)
	handler := &ListTodosHandler{Store: store, Now: func() time.Time { return fixedNow }}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", dto.ListFilters{})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.listLimit != dto.MaxListLimit {
		t.Fatalf("store limit = %d, want %d", store.listLimit, dto.MaxListLimit)
	}
	if len(got) != 2 {
		t.Fatalf("todos = %d, want 2", len(got))
	}
	overdueCount := 0
	for _, view := range got {
		if view.ID == "todo-overdue" && view.Overdue {
			overdueCount++
		}
		if view.ID == "todo-future" && view.Overdue {
			t.Fatal("future todo mapped overdue")
		}
	}
	if overdueCount != 1 {
		t.Fatalf("overdue mapping = %d, want 1", overdueCount)
	}
}

func TestListTodosPassesFiltersThrough(t *testing.T) {
	store := newFakeStore()
	handler := &ListTodosHandler{Store: store, Now: func() time.Time { return fixedNow }}
	from := fixedNow.Add(-time.Hour)
	filters := dto.ListFilters{Keyword: "周报", Status: "pending", DueFrom: &from, NoDue: true}

	if _, err := handler.Handle(context.Background(), "ws-1", "user-1", filters); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.listFilters != filters {
		t.Fatalf("filters = %#v, want %#v", store.listFilters, filters)
	}
}

func TestGetTodoReturnsMappedView(t *testing.T) {
	store := newFakeStore()
	seedTodo(t, store, "todo-1", nil)
	handler := &GetTodoHandler{Store: store, Now: func() time.Time { return fixedNow }}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", "todo-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.ID != "todo-1" || got.Status != string(domain.StatusPending) || got.Title != "提交周报" {
		t.Fatalf("view = %#v", got)
	}

	if _, err := handler.Handle(context.Background(), "ws-1", "user-1", "missing"); !errors.Is(err, domain.ErrTodoNotFound) {
		t.Fatalf("Handle(missing) error = %v, want ErrTodoNotFound", err)
	}
}

func TestDashboardComputesTodayWindowInTimezone(t *testing.T) {
	store := newFakeStore()
	store.dashboardReply = dto.DashboardSummary{PendingTotal: 3, DueToday: 1, Overdue: 1, NoDue: 1, CompletedLast7Days: 2}
	handler := &DashboardSummaryHandler{Store: store, Now: func() time.Time { return fixedNow }}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	// 2026-08-18T12:00Z is 2026-08-18 20:00 in Asia/Shanghai: the local day
	// spans [2026-08-17T16:00Z, 2026-08-18T16:00Z).
	wantStart := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 8, 18, 16, 0, 0, 0, time.UTC)
	if !store.dashboard.dueTodayStart.Equal(wantStart) || !store.dashboard.dueTodayEnd.Equal(wantEnd) {
		t.Fatalf("window = [%v, %v), want [%v, %v)", store.dashboard.dueTodayStart, store.dashboard.dueTodayEnd, wantStart, wantEnd)
	}
	if !store.dashboard.now.Equal(fixedNow) {
		t.Fatalf("store now = %v", store.dashboard.now)
	}
	if got.PendingTotal != 3 || got.DueToday != 1 || got.Overdue != 1 || got.NoDue != 1 || got.CompletedLast7Days != 2 {
		t.Fatalf("summary = %#v", got)
	}
	if got.ReminderRetrying != 0 || got.ReminderFailed != 0 || got.ReminderSucceeded != 0 || got.ReminderSuppressed != 0 {
		t.Fatalf("reminder counters = %d/%d/%d/%d, want four zeros without ReminderStats",
			got.ReminderSucceeded, got.ReminderRetrying, got.ReminderFailed, got.ReminderSuppressed)
	}
	if !got.CheckedAt.Equal(fixedNow) {
		t.Fatalf("CheckedAt = %v, want injected now", got.CheckedAt)
	}
}

func TestDashboardMapsReminderStatsCounts(t *testing.T) {
	store := newFakeStore()
	var statsWorkspaceID string
	stats := func(_ context.Context, workspaceID string) (ports.ReminderCounts, error) {
		statsWorkspaceID = workspaceID
		return ports.ReminderCounts{Succeeded: 7, Retrying: 2, Failed: 3, Suppressed: 5}, nil
	}
	handler := &DashboardSummaryHandler{Store: store, Now: func() time.Time { return fixedNow }, ReminderStats: stats}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if statsWorkspaceID != "ws-1" {
		t.Fatalf("ReminderStats workspaceID = %q, want caller workspace", statsWorkspaceID)
	}
	if got.ReminderSucceeded != 7 || got.ReminderRetrying != 2 || got.ReminderFailed != 3 || got.ReminderSuppressed != 5 {
		t.Fatalf("reminder counters = %d/%d/%d/%d, want 7/2/3/5",
			got.ReminderSucceeded, got.ReminderRetrying, got.ReminderFailed, got.ReminderSuppressed)
	}
}

func TestDashboardReminderStatsErrorPropagates(t *testing.T) {
	store := newFakeStore()
	statsErr := errors.New("reminder stats down")
	handler := &DashboardSummaryHandler{Store: store, Now: func() time.Time { return fixedNow },
		ReminderStats: func(context.Context, string) (ports.ReminderCounts, error) {
			return ports.ReminderCounts{}, statsErr
		}}

	_, err := handler.Handle(context.Background(), "ws-1", "user-1", "Asia/Shanghai")
	if !errors.Is(err, statsErr) {
		t.Fatalf("Handle() error = %v, want stats failure propagated", err)
	}
}

func TestDashboardRejectsInvalidTimezone(t *testing.T) {
	store := newFakeStore()
	handler := &DashboardSummaryHandler{Store: store, Now: func() time.Time { return fixedNow }}

	for _, timezone := range []string{"", "Not/AZone", "UTC+8"} {
		if _, err := handler.Handle(context.Background(), "ws-1", "user-1", timezone); !errors.Is(err, domain.ErrInvalidTimezone) {
			t.Fatalf("Handle(%q) error = %v, want ErrInvalidTimezone", timezone, err)
		}
	}
}

func TestSearchCandidatesCapsAtEleven(t *testing.T) {
	store := newFakeStore()
	store.candidateReply = []dto.Candidate{{TodoID: "todo-1", Title: "提交周报", Version: 1}}
	handler := &SearchCandidatesHandler{Store: store}

	got, err := handler.Handle(context.Background(), "ws-1", "user-1", "周报")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if store.candidateLimit != dto.MaxCandidateLimit {
		t.Fatalf("candidate limit = %d, want %d", store.candidateLimit, dto.MaxCandidateLimit)
	}
	if len(got) != 1 || got[0].TodoID != "todo-1" || got[0].Version != 1 {
		t.Fatalf("candidates = %#v", got)
	}
}
