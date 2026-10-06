package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	"testing"
)

func TestHistoryStoreExportAndRestoreAreOwnerScoped(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	s := NewHistoryStore(pool)
	workspace, user, id := randomID(t), randomID(t), randomID(t)
	if err := s.ImportSession(ctx, workspace, user, id, dto.HistorySession{Title: "完整会话", CreatedAt: testNow, UpdatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	message := dto.HistoryMessage{SessionID: &id, Role: "assistant", Body: "保留\n全部历史", Order: 1, CreatedAt: testNow}
	if _, err := s.ImportMessage(ctx, workspace, user, message); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ExportMessages(ctx, workspace, user, 0, 1)
	if err != nil || len(rows) != 1 || rows[0].Body != message.Body || !rows[0].CreatedAt.Equal(testNow) {
		t.Fatalf("history = %#v,%v", rows, err)
	}
	for _, scope := range [][2]string{{workspace, randomID(t)}, {randomID(t), user}} {
		rows, err := s.ExportMessages(ctx, scope[0], scope[1], 0, 10)
		if err != nil || len(rows) != 0 {
			t.Fatalf("foreign export leaked %#v,%v", rows, err)
		}
		if _, err := s.ImportMessage(ctx, scope[0], scope[1], message); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("foreign parent accepted: %v", err)
		}
	}
}
