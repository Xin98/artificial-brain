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

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

var testNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

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
	if _, err := pool.Exec(ctx, `
		truncate identity.login_challenges, identity.sessions, identity.contact_channels,
			identity.users, identity.workspaces, identity.message_outbox
	`); err != nil {
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

func seedWorkspaceAndUser(t *testing.T, pool *pgxpool.Pool, phone string) (string, string) {
	t.Helper()
	ctx := context.Background()
	wsID := randomID(t)
	if err := NewWorkspaceStore(pool).Save(ctx, domain.PersonalWorkspace{ID: wsID, CreatedAt: testNow}); err != nil {
		t.Fatalf("workspace save error = %v", err)
	}
	userID := randomID(t)
	if err := NewUserStore(pool).Save(ctx, domain.User{ID: userID, WorkspaceID: wsID, Phone: phone, CreatedAt: testNow}); err != nil {
		t.Fatalf("user save error = %v", err)
	}
	return wsID, userID
}

func TestUserStoreSaveAndByPhone(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	phone := "+8613800137000"
	_, _ = seedWorkspaceAndUser(t, pool, phone)

	got, err := NewUserStore(pool).ByPhone(ctx, phone)
	if err != nil {
		t.Fatalf("ByPhone() error = %v", err)
	}
	if got.Phone != phone || got.ID == "" || got.WorkspaceID == "" {
		t.Fatalf("user = %#v", got)
	}
	if _, err := NewUserStore(pool).ByPhone(ctx, "+19999999999"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("ByPhone(missing) error = %v, want ErrUserNotFound", err)
	}
}

func TestChallengeStoreLifecycle(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewChallengeStore(pool)
	phone := "+8613800137000"

	challenge := domain.LoginChallenge{
		ID:        randomID(t),
		Phone:     phone,
		CodeHash:  domain.HashCode("123456"),
		CreatedAt: testNow,
		ExpiresAt: testNow.Add(5 * time.Minute),
	}
	if err := store.Save(ctx, challenge); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.ActiveByPhone(ctx, phone)
	if err != nil {
		t.Fatalf("ActiveByPhone() error = %v", err)
	}
	if !got.Matches(domain.HashCode("123456")) || got.IsConsumed() {
		t.Fatalf("challenge = %#v", got)
	}

	count, err := store.CountByPhoneSince(ctx, phone, testNow.Add(-time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("count = %d, err = %v, want 1", count, err)
	}

	if err := got.Consume(testNow); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	again, err := store.ActiveByPhone(ctx, phone)
	if err != nil {
		t.Fatal(err)
	}
	if !again.IsConsumed() {
		t.Fatal("challenge not consumed after update")
	}
}

func TestSessionStoreByTokenHashAndRevoke(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewSessionStore(pool)
	_, userID := seedWorkspaceAndUser(t, pool, "+8613800137001")

	session := domain.Session{
		ID:          randomID(t),
		UserID:      userID,
		WorkspaceID: randomID(t),
		TokenHash:   domain.HashCode("token-abc"),
		CreatedAt:   testNow,
		ExpiresAt:   testNow.Add(time.Hour),
	}
	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.ByTokenHash(ctx, domain.HashCode("token-abc"))
	if err != nil {
		t.Fatalf("ByTokenHash() error = %v", err)
	}
	if got.ID != session.ID {
		t.Fatalf("session = %#v", got)
	}

	got.Revoke(testNow)
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	byID, err := store.ByID(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !byID.IsRevoked() {
		t.Fatal("session not revoked")
	}
	if _, err := store.ByTokenHash(ctx, domain.HashCode("unknown")); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("ByTokenHash(missing) error = %v, want ErrSessionNotFound", err)
	}
}

func TestChannelStoreEnforcesUniqueKindAddress(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewChannelStore(pool)
	_, userID := seedWorkspaceAndUser(t, pool, "+8613800137002")

	channel := domain.ContactChannel{
		ID:            randomID(t),
		UserID:        userID,
		WorkspaceID:   randomID(t),
		Kind:          domain.ChannelKindEmail,
		Address:       "user@example.com",
		Verified:      false,
		Enabled:       true,
		CodeHash:      domain.HashCode("222333"),
		CodeExpiresAt: ptrTime(testNow.Add(10 * time.Minute)),
		CreatedAt:     testNow,
	}
	if err := store.Save(ctx, channel); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	duplicate := channel
	duplicate.ID = randomID(t)
	if err := store.Save(ctx, duplicate); !errors.Is(err, domain.ErrChannelExists) {
		t.Fatalf("duplicate Save() error = %v, want ErrChannelExists", err)
	}

	list, err := store.ListByUser(ctx, channel.WorkspaceID, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByUser() = %d, err = %v, want 1", len(list), err)
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
