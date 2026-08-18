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
	if _, err := pool.Exec(ctx, `truncate conversation.confirmation_requests, conversation.messages restart identity`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	return pool
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
