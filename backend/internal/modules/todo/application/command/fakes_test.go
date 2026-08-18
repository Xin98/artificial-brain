package command

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

type todoKey struct {
	workspaceID string
	ownerUserID string
	todoID      string
}

// fakeTodoStore is an in-memory TodoStore. Update enforces optimistic
// concurrency like the SQL adapter: a stale expectedVersion yields
// domain.ErrConflict.
type fakeTodoStore struct {
	mu    sync.Mutex
	todos map[todoKey]domain.Todo
}

func newFakeTodoStore() *fakeTodoStore { return &fakeTodoStore{todos: map[todoKey]domain.Todo{}} }

func keyOf(workspaceID, ownerUserID, todoID string) todoKey {
	return todoKey{workspaceID, ownerUserID, todoID}
}

func (s *fakeTodoStore) Insert(_ context.Context, todo domain.Todo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos[keyOf(todo.WorkspaceID, todo.OwnerUserID, todo.ID)] = todo
	return nil
}

func (s *fakeTodoStore) Get(_ context.Context, workspaceID, ownerUserID, todoID string) (domain.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	todo, ok := s.todos[keyOf(workspaceID, ownerUserID, todoID)]
	if !ok {
		return domain.Todo{}, domain.ErrTodoNotFound
	}
	return todo, nil
}

func (s *fakeTodoStore) Update(_ context.Context, todo domain.Todo, expectedVersion int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := keyOf(todo.WorkspaceID, todo.OwnerUserID, todo.ID)
	stored, ok := s.todos[key]
	if !ok {
		return domain.ErrTodoNotFound
	}
	if stored.Version != expectedVersion {
		return domain.ErrConflict
	}
	s.todos[key] = todo
	return nil
}

func (s *fakeTodoStore) List(_ context.Context, workspaceID, ownerUserID string, filters dto.ListFilters, limit int) ([]domain.Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []domain.Todo
	for key, todo := range s.todos {
		if key.workspaceID != workspaceID || key.ownerUserID != ownerUserID {
			continue
		}
		if todo.Status == domain.StatusDeleted {
			continue
		}
		if filters.Keyword != "" && !strings.Contains(todo.Title, filters.Keyword) {
			continue
		}
		if filters.Status != "" && string(todo.Status) != filters.Status {
			continue
		}
		if filters.NoDue && todo.DueAtUTC != nil {
			continue
		}
		if filters.DueFrom != nil && (todo.DueAtUTC == nil || todo.DueAtUTC.Before(*filters.DueFrom)) {
			continue
		}
		if filters.DueTo != nil && (todo.DueAtUTC == nil || todo.DueAtUTC.After(*filters.DueTo)) {
			continue
		}
		result = append(result, todo)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *fakeTodoStore) Dashboard(_ context.Context, workspaceID, ownerUserID string, now, dueTodayStart, dueTodayEnd time.Time) (dto.DashboardSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := dto.DashboardSummary{}
	for key, todo := range s.todos {
		if key.workspaceID != workspaceID || key.ownerUserID != ownerUserID {
			continue
		}
		switch todo.Status {
		case domain.StatusPending:
			summary.PendingTotal++
			if todo.DueAtUTC == nil {
				summary.NoDue++
				continue
			}
			if todo.DueAtUTC.Before(now) {
				summary.Overdue++
			}
			if !todo.DueAtUTC.Before(dueTodayStart) && todo.DueAtUTC.Before(dueTodayEnd) {
				summary.DueToday++
			}
		case domain.StatusCompleted:
			if todo.CompletedAt != nil && !todo.CompletedAt.Before(now.Add(-7*24*time.Hour)) && !todo.CompletedAt.After(now) {
				summary.CompletedLast7Days++
			}
		}
	}
	return summary, nil
}

func (s *fakeTodoStore) SearchCandidates(_ context.Context, workspaceID, ownerUserID, keyword string, limit int) ([]dto.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []dto.Candidate
	for key, todo := range s.todos {
		if key.workspaceID != workspaceID || key.ownerUserID != ownerUserID {
			continue
		}
		if todo.Status != domain.StatusPending || !strings.Contains(todo.Title, keyword) {
			continue
		}
		result = append(result, dto.Candidate{TodoID: todo.ID, Title: todo.Title, DueAtUTC: todo.DueAtUTC, Version: todo.Version})
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *fakeTodoStore) snapshot() map[todoKey]domain.Todo {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := make(map[todoKey]domain.Todo, len(s.todos))
	for key, todo := range s.todos {
		clone[key] = todo
	}
	return clone
}

func (s *fakeTodoStore) restore(snapshot map[todoKey]domain.Todo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = snapshot
}

func (s *fakeTodoStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.todos)
}

// fakeUoW runs the unit of work against the fake store with rollback
// semantics: a failing unit restores the store snapshot, mirroring the
// database transaction the real TxRunner provides.
type fakeUoW struct {
	store *fakeTodoStore
}

func (u *fakeUoW) Run(ctx context.Context, work func(context.Context) error) error {
	snapshot := u.store.snapshot()
	if err := work(ctx); err != nil {
		u.store.restore(snapshot)
		return err
	}
	return nil
}

type plannerCall struct {
	plan   *ports.PlanReminderRequest
	revoke *ports.RevokeReminderRequest
}

type fakePlanner struct {
	calls     []plannerCall
	planErr   error
	revokeErr error
}

func newFakePlanner() *fakePlanner { return &fakePlanner{} }

func (p *fakePlanner) Plan(_ context.Context, request ports.PlanReminderRequest) error {
	if p.planErr != nil {
		return p.planErr
	}
	p.calls = append(p.calls, plannerCall{plan: &request})
	return nil
}

func (p *fakePlanner) Revoke(_ context.Context, request ports.RevokeReminderRequest) error {
	if p.revokeErr != nil {
		return p.revokeErr
	}
	p.calls = append(p.calls, plannerCall{revoke: &request})
	return nil
}

func (p *fakePlanner) plans() []ports.PlanReminderRequest {
	var result []ports.PlanReminderRequest
	for _, call := range p.calls {
		if call.plan != nil {
			result = append(result, *call.plan)
		}
	}
	return result
}

func (p *fakePlanner) revokes() []ports.RevokeReminderRequest {
	var result []ports.RevokeReminderRequest
	for _, call := range p.calls {
		if call.revoke != nil {
			result = append(result, *call.revoke)
		}
	}
	return result
}
