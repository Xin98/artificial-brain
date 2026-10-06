package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

type historyImportStore struct {
	owner    string
	saved    []dto.HistoryMessage
	sessions []dto.HistorySession
}

func (s *historyImportStore) ImportSession(_ context.Context, workspace, user, id string, r dto.HistorySession) error {
	s.sessions = append(s.sessions, r)
	return nil
}
func (s *historyImportStore) ImportMessage(_ context.Context, workspace, user string, r dto.HistoryMessage) (string, error) {
	if user != s.owner {
		return "", domain.ErrSessionNotFound
	}
	s.saved = append(s.saved, r)
	return "1", nil
}
func TestHistoryImportPreservesTranscriptAndOwnerScope(t *testing.T) {
	store := &historyImportStore{owner: "owner"}
	h := ImportHistoryHandler{Store: store, NewID: func() string { return "new-session" }}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	id, err := h.ImportSession(context.Background(), "workspace", "owner", dto.HistorySession{Title: "会话", CreatedAt: at, UpdatedAt: at})
	if err != nil || id != "new-session" {
		t.Fatalf("session = %s,%v", id, err)
	}
	r := dto.HistoryMessage{SessionID: &id, Role: "assistant", Body: "删除的历史回复\n原文", Order: 1, CreatedAt: at}
	_, err = h.ImportMessage(context.Background(), "workspace", "owner", r)
	if err != nil || len(store.saved) != 1 || store.saved[0].Body != r.Body || !store.saved[0].CreatedAt.Equal(at) {
		t.Fatalf("message = %#v,%v", store.saved, err)
	}
	_, err = h.ImportMessage(context.Background(), "workspace", "another", r)
	if !errors.Is(err, domain.ErrSessionNotFound) || len(store.saved) != 1 {
		t.Fatalf("foreign session accepted: %v", err)
	}
}
