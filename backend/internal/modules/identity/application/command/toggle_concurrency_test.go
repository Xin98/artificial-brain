package command

import (
	"context"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// interleavedChannelStore forces a verification/resend write between a
// toggle's snapshot read and write, reproducing the lost-update race.
type interleavedChannelStore struct {
	*fakeChannelStore
	concurrent domain.ContactChannel
}

func (s *interleavedChannelStore) ByID(ctx context.Context, workspaceID, userID, channelID string) (domain.ContactChannel, error) {
	snapshot, err := s.fakeChannelStore.ByID(ctx, workspaceID, userID, channelID)
	if err == nil {
		s.channels[channelID] = s.concurrent
	}
	return snapshot, err
}
func (s *interleavedChannelStore) SetEnabled(ctx context.Context, workspaceID, userID, channelID string, enabled bool) (domain.ContactChannel, error) {
	if _, err := s.fakeChannelStore.ByID(ctx, workspaceID, userID, channelID); err != nil {
		return domain.ContactChannel{}, err
	}
	s.channels[channelID] = s.concurrent
	current := s.channels[channelID]
	current.Enabled = enabled
	s.channels[channelID] = current
	return current, nil
}

func TestChannelToggleRetainsConcurrentVerificationAndCodeRotation(t *testing.T) {
	for _, verified := range []bool{false, true} {
		expires := testNow.Add(10 * time.Minute)
		initial := domain.ContactChannel{ID: "c-1", WorkspaceID: testPrincipal.WorkspaceID, UserID: testPrincipal.UserID, Kind: domain.ChannelKindEmail, Address: "user@example.com", Enabled: true, CodeHash: domain.HashCode("111222"), CodeExpiresAt: &expires, CreatedAt: testNow}
		freshExpires := testNow.Add(20 * time.Minute)
		concurrent := initial
		concurrent.Verified, concurrent.CodeHash, concurrent.CodeExpiresAt = verified, domain.HashCode("444555"), &freshExpires
		store := &interleavedChannelStore{fakeChannelStore: newFakeChannelStore(), concurrent: concurrent}
		store.channels[initial.ID] = initial
		h := &SetChannelEnabledHandler{Channels: store}
		if _, err := h.Handle(context.Background(), testPrincipal, initial.ID, false); err != nil {
			t.Fatal(err)
		}
		got := store.channels[initial.ID]
		if got.Enabled || got.Verified != verified || got.CodeHash != concurrent.CodeHash || !got.CodeExpiresAt.Equal(freshExpires) {
			t.Fatalf("toggle overwrote concurrent state: %#v", got)
		}
	}
}
