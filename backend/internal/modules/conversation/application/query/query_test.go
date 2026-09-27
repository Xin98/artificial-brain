package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

var fixedNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func ctx() context.Context { return context.Background() }

type fakeSessionStore struct {
	sessions  map[string]domain.Session
	order     []string
	listLimit int
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]domain.Session{}}
}

func (s *fakeSessionStore) seed(session domain.Session) {
	s.sessions[session.ID] = session
	s.order = append(s.order, session.ID)
}

func (s *fakeSessionStore) Create(_ context.Context, session domain.Session) error {
	s.seed(session)
	return nil
}

func (s *fakeSessionStore) Get(_ context.Context, workspaceID, userID, sessionID string) (domain.Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok || session.WorkspaceID != workspaceID || session.UserID != userID {
		return domain.Session{}, domain.ErrSessionNotFound
	}
	return session, nil
}

func (s *fakeSessionStore) List(_ context.Context, workspaceID, userID string, limit int) ([]domain.Session, error) {
	s.listLimit = limit
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

func (s *fakeSessionStore) Rename(_ context.Context, _, _, _, _ string, _ time.Time) error {
	return domain.ErrSessionNotFound
}

func (s *fakeSessionStore) Delete(_ context.Context, _, _, _ string) error {
	return domain.ErrSessionNotFound
}

func (s *fakeSessionStore) Touch(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}

type fakeMessageLog struct {
	entries   []ports.MessageLogEntry
	gotLimit  int
	gotSessID string
}

func (l *fakeMessageLog) Append(_ context.Context, _ ports.MessageLog) error { return nil }

func (l *fakeMessageLog) ListBySession(_ context.Context, _, _, sessionID string, limit int) ([]ports.MessageLogEntry, error) {
	l.gotSessID, l.gotLimit = sessionID, limit
	if len(l.entries) > limit {
		return l.entries[len(l.entries)-limit:], nil
	}
	return l.entries, nil
}

func TestListSessionsProjectsAndBounds(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.seed(domain.Session{ID: "s-1", WorkspaceID: "ws-1", UserID: "user-1", Title: "旧会话", CreatedAt: fixedNow.Add(-2 * time.Hour), UpdatedAt: fixedNow.Add(-2 * time.Hour)})
	sessions.seed(domain.Session{ID: "s-2", WorkspaceID: "ws-1", UserID: "user-1", Title: "新会话", CreatedAt: fixedNow, UpdatedAt: fixedNow})
	sessions.seed(domain.Session{ID: "s-3", WorkspaceID: "ws-2", UserID: "user-1", Title: "别的工作区", CreatedAt: fixedNow, UpdatedAt: fixedNow})
	handler := &ListSessionsHandler{Sessions: sessions}

	got, err := handler.Handle(ctx(), "ws-1", "user-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if sessions.listLimit != MaxListedSessions {
		t.Fatalf("limit = %d, want %d", sessions.listLimit, MaxListedSessions)
	}
	if len(got.Sessions) != 2 || got.Sessions[0].ID != "s-2" || got.Sessions[1].ID != "s-1" {
		t.Fatalf("sessions = %#v, want scoped list, most recent first", got.Sessions)
	}
	if got.Sessions[0].Title != "新会话" || !got.Sessions[0].UpdatedAt.Equal(fixedNow) {
		t.Fatalf("view = %#v", got.Sessions[0])
	}
}

func TestGetHistoryReplaysTranscript(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.seed(domain.Session{ID: "s-1", WorkspaceID: "ws-1", UserID: "user-1", Title: "周报", CreatedAt: fixedNow, UpdatedAt: fixedNow})
	chat := "chat"
	log := &fakeMessageLog{entries: []ports.MessageLogEntry{
		{ID: "10", SessionID: "s-1", Role: ports.RoleUser, Body: "你好", ResolvedIntent: &chat, CreatedAt: fixedNow},
		{ID: "11", SessionID: "s-1", Role: ports.RoleAssistant, Body: "你好！有什么可以帮你？", CreatedAt: fixedNow},
	}}
	handler := &GetHistoryHandler{Sessions: sessions, Messages: log}

	got, err := handler.Handle(ctx(), "ws-1", "user-1", "s-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got.SessionID != "s-1" || got.Title != "周报" {
		t.Fatalf("envelope = %#v", got)
	}
	if log.gotLimit != MaxHistoryMessages || log.gotSessID != "s-1" {
		t.Fatalf("read = %#v, want ListBySession(s-1, %d)", log, MaxHistoryMessages)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %#v", got.Messages)
	}
	if got.Messages[0].ID != "10" || got.Messages[0].Role != "user" || got.Messages[0].ResolvedIntent == nil || *got.Messages[0].ResolvedIntent != "chat" {
		t.Fatalf("user view = %#v", got.Messages[0])
	}
	if got.Messages[1].ID != "11" || got.Messages[1].Role != "assistant" || got.Messages[1].Body != "你好！有什么可以帮你？" || got.Messages[1].ResolvedIntent != nil {
		t.Fatalf("assistant view = %#v", got.Messages[1])
	}

	if _, err := handler.Handle(ctx(), "ws-1", "user-1", "s-x"); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Handle(unknown) error = %v, want ErrSessionNotFound", err)
	}
}
