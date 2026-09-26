package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "..", "..", "..", "..", "deploy", "migrations")
	if err := database.RunMigrations(ctx, url, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := database.OpenPool(ctx, url)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `truncate conversation.confirmation_requests, conversation.messages, conversation.sessions restart identity`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	return pool
}

func newSession(t *testing.T, id, workspaceID, userID, title string, now time.Time) domain.Session {
	t.Helper()
	session, err := domain.NewSession(id, workspaceID, userID, title, now)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	return session
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func newConfirmation(t *testing.T, id, workspaceID, userID, todoID string, ttl time.Duration) domain.ConfirmationRequest {
	t.Helper()
	confirmation, err := domain.NewConfirmationRequest(id, workspaceID, userID, domain.IntentTodoDelete, todoID, 1, testNow, ttl)
	if err != nil {
		t.Fatalf("NewConfirmationRequest() error = %v", err)
	}
	return confirmation
}

func TestConfirmationStoreSaveGetConsumeOnce(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewConfirmationStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	confirmation := newConfirmation(t, randomID(t), workspaceID, ownerUserID, randomID(t), 5*time.Minute)
	if err := store.Save(ctx, confirmation); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Get(ctx, workspaceID, ownerUserID, confirmation.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.TodoID != confirmation.TodoID || got.TodoVersion != 1 || got.Intent != domain.IntentTodoDelete {
		t.Fatalf("Get() = %#v", got)
	}
	if !got.ExpiresAt.Equal(confirmation.ExpiresAt) || got.ConsumedAt != nil {
		t.Fatalf("Get() window = %#v", got)
	}

	if err := store.Consume(ctx, workspaceID, ownerUserID, confirmation.ID, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	consumed, err := store.Get(ctx, workspaceID, ownerUserID, confirmation.ID)
	if err != nil {
		t.Fatalf("Get(after consume) error = %v", err)
	}
	if consumed.ConsumedAt == nil || !consumed.ConsumedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("consumed_at = %v", consumed.ConsumedAt)
	}

	// The conditional consume is single-use.
	if err := store.Consume(ctx, workspaceID, ownerUserID, confirmation.ID, testNow.Add(2*time.Minute)); !errors.Is(err, domain.ErrConfirmationConsumed) {
		t.Fatalf("second Consume() error = %v, want ErrConfirmationConsumed", err)
	}

	// An expired confirmation cannot be consumed.
	expired := newConfirmation(t, randomID(t), workspaceID, ownerUserID, randomID(t), -time.Minute)
	if err := store.Save(ctx, expired); err != nil {
		t.Fatalf("Save(expired) error = %v", err)
	}
	if err := store.Consume(ctx, workspaceID, ownerUserID, expired.ID, testNow); !errors.Is(err, domain.ErrConfirmationExpired) {
		t.Fatalf("Consume(expired) error = %v, want ErrConfirmationExpired", err)
	}
}

func TestConfirmationStoreScopesByWorkspaceAndUser(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewConfirmationStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)
	confirmation := newConfirmation(t, randomID(t), workspaceID, ownerUserID, randomID(t), 5*time.Minute)
	if err := store.Save(ctx, confirmation); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := store.Get(ctx, randomID(t), ownerUserID, confirmation.ID); !errors.Is(err, domain.ErrConfirmationNotFound) {
		t.Fatalf("Get(other workspace) error = %v, want ErrConfirmationNotFound", err)
	}
	if _, err := store.Get(ctx, workspaceID, randomID(t), confirmation.ID); !errors.Is(err, domain.ErrConfirmationNotFound) {
		t.Fatalf("Get(other user) error = %v, want ErrConfirmationNotFound", err)
	}
	if err := store.Consume(ctx, randomID(t), ownerUserID, confirmation.ID, testNow); !errors.Is(err, domain.ErrConfirmationNotFound) {
		t.Fatalf("Consume(other workspace) error = %v, want ErrConfirmationNotFound", err)
	}
}

func TestMessageLogAppendListOrderingAndIsolation(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewMessageLogStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	intents := []string{"todo.create", "todo.list", "todo.delete"}
	for index, intent := range intents {
		resolved := intent
		message := ports.MessageLog{
			WorkspaceID:    workspaceID,
			UserID:         ownerUserID,
			Role:           ports.RoleUser,
			Body:           fmt.Sprintf("消息%d", index),
			ResolvedIntent: &resolved,
			CreatedAt:      testNow.Add(time.Duration(index) * time.Second),
		}
		if err := store.Append(ctx, message); err != nil {
			t.Fatalf("Append(%d) error = %v", index, err)
		}
	}
	// A turn without a resolved intent is still appended.
	if err := store.Append(ctx, ports.MessageLog{
		WorkspaceID: workspaceID, UserID: ownerUserID, Role: ports.RoleUser, Body: "未解析", CreatedAt: testNow.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("Append(unresolved) error = %v", err)
	}
	// Another workspace's turn must not leak into ws-1 reads.
	if err := store.Append(ctx, ports.MessageLog{
		WorkspaceID: randomID(t), UserID: ownerUserID, Role: ports.RoleUser, Body: "别的工作区", CreatedAt: testNow,
	}); err != nil {
		t.Fatalf("Append(ws-2) error = %v", err)
	}

	messages, err := store.ListByUser(ctx, workspaceID, ownerUserID)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("messages = %d, want 4 (ws-2 excluded)", len(messages))
	}
	for index, message := range messages[:3] {
		if message.ResolvedIntent == nil || *message.ResolvedIntent != intents[index] {
			t.Fatalf("message %d = %#v, want resolved %q in order", index, message, intents[index])
		}
	}
	if messages[3].ResolvedIntent != nil {
		t.Fatalf("unresolved turn carries intent %#v", messages[3].ResolvedIntent)
	}

	empty, err := store.ListByUser(ctx, randomID(t), ownerUserID)
	if err != nil || len(empty) != 0 {
		t.Fatalf("ListByUser(other workspace) = %d, err = %v, want 0", len(empty), err)
	}
}

func TestSessionStoreCRUDOrderingAndIsolation(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewSessionStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	first := newSession(t, randomID(t), workspaceID, ownerUserID, "第一个会话", testNow)
	second := newSession(t, randomID(t), workspaceID, ownerUserID, "第二个会话", testNow.Add(time.Minute))
	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	if err := store.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}

	got, err := store.Get(ctx, workspaceID, ownerUserID, first.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "第一个会话" || !got.CreatedAt.Equal(testNow) || !got.UpdatedAt.Equal(testNow) {
		t.Fatalf("Get() = %#v", got)
	}

	// Listing orders by updated_at desc: touching the first session flips
	// the order.
	list, err := store.List(ctx, workspaceID, ownerUserID, 100)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("List() = %#v, want second before first", list)
	}
	if err := store.Touch(ctx, workspaceID, ownerUserID, first.ID, testNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	list, err = store.List(ctx, workspaceID, ownerUserID, 100)
	if err != nil {
		t.Fatalf("List(after touch) error = %v", err)
	}
	if list[0].ID != first.ID || !list[0].UpdatedAt.Equal(testNow.Add(2*time.Minute)) {
		t.Fatalf("List(after touch) = %#v, want the touched session first", list)
	}
	// The limit bounds the listing.
	limited, err := store.List(ctx, workspaceID, ownerUserID, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("List(limit 1) = %d, err = %v, want 1", len(limited), err)
	}

	if err := store.Rename(ctx, workspaceID, ownerUserID, second.ID, "改名后的会话", testNow.Add(3*time.Minute)); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	renamed, err := store.Get(ctx, workspaceID, ownerUserID, second.ID)
	if err != nil {
		t.Fatalf("Get(after rename) error = %v", err)
	}
	if renamed.Title != "改名后的会话" || !renamed.UpdatedAt.Equal(testNow.Add(3*time.Minute)) {
		t.Fatalf("renamed = %#v", renamed)
	}

	// Scoped misses: another workspace or user cannot see or mutate.
	if _, err := store.Get(ctx, randomID(t), ownerUserID, first.ID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Get(other workspace) error = %v, want ErrSessionNotFound", err)
	}
	if _, err := store.Get(ctx, workspaceID, randomID(t), first.ID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Get(other user) error = %v, want ErrSessionNotFound", err)
	}
	if err := store.Rename(ctx, randomID(t), ownerUserID, first.ID, "越权", testNow); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Rename(other workspace) error = %v, want ErrSessionNotFound", err)
	}
	if err := store.Touch(ctx, workspaceID, randomID(t), first.ID, testNow); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Touch(other user) error = %v, want ErrSessionNotFound", err)
	}
	if err := store.Delete(ctx, randomID(t), ownerUserID, first.ID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Delete(other workspace) error = %v, want ErrSessionNotFound", err)
	}
	if err := store.Delete(ctx, workspaceID, ownerUserID, randomID(t)); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Delete(missing) error = %v, want ErrSessionNotFound", err)
	}

	if err := store.Delete(ctx, workspaceID, ownerUserID, second.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, workspaceID, ownerUserID, second.ID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("Get(after delete) error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionDeleteCascadesTranscript(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	sessions := NewSessionStore(pool)
	messages := NewMessageLogStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)
	session := newSession(t, randomID(t), workspaceID, ownerUserID, "会被删除", testNow)
	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sessionID := session.ID
	if err := messages.Append(ctx, ports.MessageLog{
		WorkspaceID: workspaceID, UserID: ownerUserID, Role: ports.RoleUser,
		Body: "会话内消息", SessionID: &sessionID, CreatedAt: testNow,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := sessions.Delete(ctx, workspaceID, ownerUserID, session.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	history, err := messages.ListBySession(ctx, workspaceID, ownerUserID, session.ID, 200)
	if err != nil {
		t.Fatalf("ListBySession() error = %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("history after cascade = %d rows, want 0", len(history))
	}
}

func TestMessageLogSessionScopingAndWindow(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewMessageLogStore(pool)
	sessions := NewSessionStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)
	own := newSession(t, randomID(t), workspaceID, ownerUserID, "本会话", testNow)
	other := newSession(t, randomID(t), workspaceID, ownerUserID, "别会话", testNow)
	for _, session := range []domain.Session{own, other} {
		if err := sessions.Create(ctx, session); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	ownID, otherID := own.ID, other.ID
	resolved := "chat"
	for index := 0; index < 5; index++ {
		role := ports.RoleUser
		if index%2 == 1 {
			role = ports.RoleAssistant
		}
		message := ports.MessageLog{
			WorkspaceID: workspaceID, UserID: ownerUserID, Role: role,
			Body: fmt.Sprintf("第%d条", index), SessionID: &ownID, CreatedAt: testNow,
		}
		if role == ports.RoleUser {
			message.ResolvedIntent = &resolved
		}
		if err := store.Append(ctx, message); err != nil {
			t.Fatalf("Append(%d) error = %v", index, err)
		}
	}
	// Another session's row and a legacy NULL-session row must not leak in.
	if err := store.Append(ctx, ports.MessageLog{
		WorkspaceID: workspaceID, UserID: ownerUserID, Role: ports.RoleUser,
		Body: "别会话的消息", SessionID: &otherID, CreatedAt: testNow,
	}); err != nil {
		t.Fatalf("Append(other session) error = %v", err)
	}
	if err := store.Append(ctx, ports.MessageLog{
		WorkspaceID: workspaceID, UserID: ownerUserID, Role: ports.RoleUser,
		Body: "历史遗留审计行", CreatedAt: testNow,
	}); err != nil {
		t.Fatalf("Append(legacy) error = %v", err)
	}

	history, err := store.ListBySession(ctx, workspaceID, ownerUserID, ownID, 200)
	if err != nil {
		t.Fatalf("ListBySession() error = %v", err)
	}
	if len(history) != 5 {
		t.Fatalf("history = %d rows, want 5", len(history))
	}
	for index, entry := range history {
		if entry.Body != fmt.Sprintf("第%d条", index) {
			t.Fatalf("history[%d] = %#v, want ascending insertion order", index, entry)
		}
		if entry.SessionID != ownID || entry.ID == "" {
			t.Fatalf("history[%d] identity = %#v", index, entry)
		}
		wantRole := ports.RoleUser
		if index%2 == 1 {
			wantRole = ports.RoleAssistant
		}
		if entry.Role != wantRole {
			t.Fatalf("history[%d].Role = %q, want %q", index, entry.Role, wantRole)
		}
	}
	if history[0].ResolvedIntent == nil || *history[0].ResolvedIntent != "chat" {
		t.Fatalf("user row lost its resolved intent: %#v", history[0])
	}
	if history[1].ResolvedIntent != nil {
		t.Fatalf("assistant row carries intent %#v", history[1].ResolvedIntent)
	}

	// The limit keeps the latest rows, still ascending.
	window, err := store.ListBySession(ctx, workspaceID, ownerUserID, ownID, 2)
	if err != nil {
		t.Fatalf("ListBySession(limit 2) error = %v", err)
	}
	if len(window) != 2 || window[0].Body != "第3条" || window[1].Body != "第4条" {
		t.Fatalf("window = %#v, want the latest two ascending", window)
	}

	// Scoped isolation.
	if leaked, err := store.ListBySession(ctx, randomID(t), ownerUserID, ownID, 200); err != nil || len(leaked) != 0 {
		t.Fatalf("ListBySession(other workspace) = %d, err = %v, want 0", len(leaked), err)
	}
	if leaked, err := store.ListBySession(ctx, workspaceID, ownerUserID, otherID, 200); err != nil || len(leaked) != 1 {
		t.Fatalf("ListBySession(other session) = %d, err = %v, want 1", len(leaked), err)
	}
}
