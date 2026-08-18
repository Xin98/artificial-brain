package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

var testNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return testNow }

type fakeSessionStore struct {
	sessions map[string]domain.Session
}

func (s *fakeSessionStore) Save(_ context.Context, session domain.Session) error {
	s.sessions[session.ID] = session
	return nil
}

func (s *fakeSessionStore) Update(_ context.Context, session domain.Session) error {
	s.sessions[session.ID] = session
	return nil
}

func (s *fakeSessionStore) ByID(_ context.Context, id string) (domain.Session, error) {
	if session, ok := s.sessions[id]; ok {
		return session, nil
	}
	return domain.Session{}, domain.ErrSessionNotFound
}

func (s *fakeSessionStore) ByTokenHash(_ context.Context, tokenHash string) (domain.Session, error) {
	for _, session := range s.sessions {
		if session.TokenHash == tokenHash {
			return session, nil
		}
	}
	return domain.Session{}, domain.ErrSessionNotFound
}

func TestAuthenticateResolvesPrincipal(t *testing.T) {
	store := &fakeSessionStore{sessions: map[string]domain.Session{
		"s1": {
			ID:          "s1",
			UserID:      "u1",
			WorkspaceID: "w1",
			TokenHash:   domain.HashCode("token-abc"),
			CreatedAt:   testNow,
			ExpiresAt:   testNow.Add(time.Hour),
		},
	}}
	q := &SessionQuery{Sessions: store, Now: fixedNow}

	principal, err := q.Authenticate(context.Background(), "token-abc")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.UserID != "u1" || principal.WorkspaceID != "w1" || principal.SessionID != "s1" {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestAuthenticateRejectsUnknownExpiredAndRevoked(t *testing.T) {
	store := &fakeSessionStore{sessions: map[string]domain.Session{
		"expired": {
			ID: "expired", UserID: "u1", WorkspaceID: "w1",
			TokenHash: domain.HashCode("expired-token"),
			CreatedAt: testNow, ExpiresAt: testNow.Add(-time.Minute),
		},
		"revoked": {
			ID: "revoked", UserID: "u1", WorkspaceID: "w1",
			TokenHash: domain.HashCode("revoked-token"),
			CreatedAt: testNow, ExpiresAt: testNow.Add(time.Hour),
			RevokedAt: &testNow,
		},
	}}
	q := &SessionQuery{Sessions: store, Now: fixedNow}

	if _, err := q.Authenticate(context.Background(), "unknown"); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("unknown token error = %v, want ErrSessionNotFound", err)
	}
	if _, err := q.Authenticate(context.Background(), "expired-token"); !errors.Is(err, domain.ErrSessionInactive) {
		t.Fatalf("expired error = %v, want ErrSessionInactive", err)
	}
	if _, err := q.Authenticate(context.Background(), "revoked-token"); !errors.Is(err, domain.ErrSessionInactive) {
		t.Fatalf("revoked error = %v, want ErrSessionInactive", err)
	}
}

type fakeChannelStore struct {
	channels []domain.ContactChannel
}

func (s *fakeChannelStore) Save(_ context.Context, c domain.ContactChannel) error {
	s.channels = append(s.channels, c)
	return nil
}

func (s *fakeChannelStore) Update(_ context.Context, c domain.ContactChannel) error { return nil }

func (s *fakeChannelStore) ByID(_ context.Context, workspaceID, userID, id string) (domain.ContactChannel, error) {
	for _, c := range s.channels {
		if c.ID == id && c.UserID == userID && c.WorkspaceID == workspaceID {
			return c, nil
		}
	}
	return domain.ContactChannel{}, domain.ErrChannelNotFound
}

func (s *fakeChannelStore) ListByUser(_ context.Context, workspaceID, userID string) ([]domain.ContactChannel, error) {
	var result []domain.ContactChannel
	for _, c := range s.channels {
		if c.UserID == userID && c.WorkspaceID == workspaceID {
			result = append(result, c)
		}
	}
	return result, nil
}

func TestGetContactChannelsScopedToUser(t *testing.T) {
	store := &fakeChannelStore{channels: []domain.ContactChannel{
		{ID: "c1", UserID: "u1", WorkspaceID: "w1", Kind: domain.ChannelKindEmail, Address: "a@example.com", Verified: true, Enabled: true, CreatedAt: testNow},
		{ID: "c2", UserID: "u2", WorkspaceID: "w1", Kind: domain.ChannelKindSMS, Address: "+8613800137001", CreatedAt: testNow},
	}}
	q := &ChannelsQuery{Channels: store}

	views, err := q.GetContactChannels(context.Background(), dto.Principal{UserID: "u1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("GetContactChannels() error = %v", err)
	}
	if len(views) != 1 || views[0].ID != "c1" {
		t.Fatalf("views = %#v", views)
	}
}
