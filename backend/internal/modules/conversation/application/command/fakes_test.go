package command

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

type fakeModel struct {
	input    *ports.MessageInput
	proposal json.RawMessage
	err      error
}

func (m *fakeModel) Propose(_ context.Context, in ports.MessageInput) (json.RawMessage, error) {
	m.input = &in
	return m.proposal, m.err
}

type fakeTodoGateway struct {
	createRequests []tododto.CreateTodoRequest
	createdTodo    tododto.Todo
	createErr      error
	listFilters    []tododto.ListFilters
	listedTodos    []tododto.Todo
	listErr        error
	candidateCalls []string
	candidates     []tododto.Candidate
	candidatesErr  error
	getCalls       []string
	gottenTodo     tododto.Todo
	getErr         error
	deleteRequests []tododto.DeleteTodoRequest
	deletedTodo    tododto.Todo
	deleteErr      error
}

func (g *fakeTodoGateway) CreateTodo(_ context.Context, request tododto.CreateTodoRequest) (tododto.Todo, error) {
	g.createRequests = append(g.createRequests, request)
	return g.createdTodo, g.createErr
}

func (g *fakeTodoGateway) ListTodos(_ context.Context, _, _ string, filters tododto.ListFilters) ([]tododto.Todo, error) {
	g.listFilters = append(g.listFilters, filters)
	return g.listedTodos, g.listErr
}

func (g *fakeTodoGateway) SearchCandidates(_ context.Context, _, _, keyword string) ([]tododto.Candidate, error) {
	g.candidateCalls = append(g.candidateCalls, keyword)
	return g.candidates, g.candidatesErr
}

func (g *fakeTodoGateway) GetTodo(_ context.Context, _, _, todoID string) (tododto.Todo, error) {
	g.getCalls = append(g.getCalls, todoID)
	return g.gottenTodo, g.getErr
}

func (g *fakeTodoGateway) DeleteTodo(_ context.Context, request tododto.DeleteTodoRequest) (tododto.Todo, error) {
	g.deleteRequests = append(g.deleteRequests, request)
	return g.deletedTodo, g.deleteErr
}

type fakeConfirmationStore struct {
	mu            sync.Mutex
	confirmations map[string]domain.ConfirmationRequest
	saveErr       error
	getErr        error
	consumeErr    error
	consumedAt    map[string]time.Time
}

func newFakeConfirmationStore() *fakeConfirmationStore {
	return &fakeConfirmationStore{
		confirmations: map[string]domain.ConfirmationRequest{},
		consumedAt:    map[string]time.Time{},
	}
}

func (s *fakeConfirmationStore) Save(_ context.Context, confirmation domain.ConfirmationRequest) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confirmations[confirmation.ID] = confirmation
	return nil
}

func (s *fakeConfirmationStore) Get(_ context.Context, workspaceID, userID, confirmationID string) (domain.ConfirmationRequest, error) {
	if s.getErr != nil {
		return domain.ConfirmationRequest{}, s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	confirmation, ok := s.confirmations[confirmationID]
	if !ok || confirmation.WorkspaceID != workspaceID || confirmation.UserID != userID {
		return domain.ConfirmationRequest{}, domain.ErrConfirmationNotFound
	}
	return confirmation, nil
}

func (s *fakeConfirmationStore) Consume(_ context.Context, workspaceID, userID, confirmationID string, now time.Time) error {
	if s.consumeErr != nil {
		return s.consumeErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	confirmation, ok := s.confirmations[confirmationID]
	if !ok || confirmation.WorkspaceID != workspaceID || confirmation.UserID != userID {
		return domain.ErrConfirmationNotFound
	}
	if _, consumed := s.consumedAt[confirmationID]; consumed {
		return domain.ErrConfirmationConsumed
	}
	if confirmation.IsExpired(now) {
		return domain.ErrConfirmationExpired
	}
	s.consumedAt[confirmationID] = now
	return nil
}

type fakeMessageLog struct {
	messages []ports.MessageLog
}

func (l *fakeMessageLog) Append(_ context.Context, message ports.MessageLog) error {
	l.messages = append(l.messages, message)
	return nil
}

type fakeUoW struct{}

func (fakeUoW) Run(ctx context.Context, work func(context.Context) error) error { return work(ctx) }
