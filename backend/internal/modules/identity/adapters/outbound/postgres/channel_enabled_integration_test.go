package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

func TestChannelTogglePreservesLockedConcurrentCodeRotation(t *testing.T) {
	pool := setupTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	workspace, user := seedWorkspaceAndUser(t, pool, "+8613800137100")
	store := NewChannelStore(pool)
	expires := testNow.Add(10 * time.Minute)
	channel := domain.ContactChannel{ID: randomID(t), WorkspaceID: workspace, UserID: user, Kind: domain.ChannelKindEmail, Address: "toggle@example.com", Enabled: true, CodeHash: domain.HashCode("111222"), CodeExpiresAt: &expires, CreatedAt: testNow}
	if err := store.Save(ctx, channel); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	freshExpires, freshHash := testNow.Add(20*time.Minute), domain.HashCode("444555")
	if _, err := tx.Exec(ctx, `update identity.contact_channels set code_hash=$2,code_expires_at=$3,verified=true where id=$1`, channel.ID, freshHash, freshExpires); err != nil {
		t.Fatal(err)
	}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		_, err := store.SetEnabled(ctx, workspace, user, channel.ID, false)
		done <- err
	}()
	<-started
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, err := store.ByID(ctx, workspace, user, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !got.Verified || got.CodeHash != freshHash || !got.CodeExpiresAt.Equal(freshExpires) {
		t.Fatalf("concurrent toggle lost rotation/verification: %#v", got)
	}
}
