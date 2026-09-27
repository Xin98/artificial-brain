package command

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

type fakeModel struct {
	input *ports.MessageInput
	turn  json.RawMessage
	err   error
}

func (m *fakeModel) Complete(_ context.Context, in ports.MessageInput) (json.RawMessage, error) {
	m.input = &in
	return m.turn, m.err
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
	messages  []ports.MessageLog
	listCalls []fakeHistoryCall
	listErr   error
}

type fakeHistoryCall struct {
	sessionID string
	limit     int
}

func (l *fakeMessageLog) Append(_ context.Context, message ports.MessageLog) error {
	l.messages = append(l.messages, message)
	return nil
}

func (l *fakeMessageLog) ListBySession(_ context.Context, _, _, sessionID string, limit int) ([]ports.MessageLogEntry, error) {
	l.listCalls = append(l.listCalls, fakeHistoryCall{sessionID: sessionID, limit: limit})
	if l.listErr != nil {
		return nil, l.listErr
	}
	// Return the recorded transcript of the session, ascending, honoring
	// the window limit like the postgres adapter does.
	var entries []ports.MessageLogEntry
	for index, message := range l.messages {
		if message.SessionID == nil || *message.SessionID != sessionID {
			continue
		}
		entries = append(entries, ports.MessageLogEntry{
			ID:             strconv.Itoa(index + 1),
			SessionID:      sessionID,
			Role:           message.Role,
			Body:           message.Body,
			ResolvedIntent: message.ResolvedIntent,
			CreatedAt:      message.CreatedAt,
		})
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

type fakeSessionStore struct {
	mu       sync.Mutex
	sessions map[string]domain.Session
	order    []string
	nextID   int
	getErr   error
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]domain.Session{}}
}

func (s *fakeSessionStore) Create(_ context.Context, session domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	s.order = append(s.order, session.ID)
	return nil
}

func (s *fakeSessionStore) Get(_ context.Context, workspaceID, userID, sessionID string) (domain.Session, error) {
	if s.getErr != nil {
		return domain.Session{}, s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.WorkspaceID != workspaceID || session.UserID != userID {
		return domain.Session{}, domain.ErrSessionNotFound
	}
	return session, nil
}

func (s *fakeSessionStore) List(_ context.Context, workspaceID, userID string, limit int) ([]domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sessions []domain.Session
	for index := len(s.order) - 1; index >= 0; index-- {
		session := s.sessions[s.order[index]]
		if session.WorkspaceID == workspaceID && session.UserID == userID {
			sessions = append(sessions, session)
		}
	}
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func (s *fakeSessionStore) Rename(_ context.Context, workspaceID, userID, sessionID, title string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.WorkspaceID != workspaceID || session.UserID != userID {
		return domain.ErrSessionNotFound
	}
	session.Title = title
	session.UpdatedAt = now
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) Delete(_ context.Context, workspaceID, userID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.WorkspaceID != workspaceID || session.UserID != userID {
		return domain.ErrSessionNotFound
	}
	delete(s.sessions, sessionID)
	return nil
}

func (s *fakeSessionStore) Touch(_ context.Context, workspaceID, userID, sessionID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.WorkspaceID != workspaceID || session.UserID != userID {
		return domain.ErrSessionNotFound
	}
	session.UpdatedAt = now
	s.sessions[sessionID] = session
	return nil
}

func (s *fakeSessionStore) takeID() string {
	s.nextID++
	return "session-" + strconv.Itoa(s.nextID)
}

type fakeUoW struct{}

func (fakeUoW) Run(ctx context.Context, work func(context.Context) error) error { return work(ctx) }
