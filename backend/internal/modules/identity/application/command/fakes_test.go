package command

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

type fakeChallengeStore struct {
	mu         sync.Mutex
	challenges map[string]domain.LoginChallenge
}

func newFakeChallengeStore() *fakeChallengeStore {
	return &fakeChallengeStore{challenges: map[string]domain.LoginChallenge{}}
}

func (s *fakeChallengeStore) Save(_ context.Context, c domain.LoginChallenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenges[c.ID] = c
	return nil
}

func (s *fakeChallengeStore) Update(_ context.Context, c domain.LoginChallenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenges[c.ID] = c
	return nil
}

func (s *fakeChallengeStore) ActiveByPhone(_ context.Context, phone string) (domain.LoginChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latestUnconsumed *domain.LoginChallenge
	var latestAny *domain.LoginChallenge
	for i := range s.challenges {
		c := s.challenges[i]
		if c.Phone != phone {
			continue
		}
		if latestAny == nil || !c.CreatedAt.Before(latestAny.CreatedAt) {
			cc := c
			latestAny = &cc
		}
		if !c.IsConsumed() && (latestUnconsumed == nil || !c.CreatedAt.Before(latestUnconsumed.CreatedAt)) {
			cc := c
			latestUnconsumed = &cc
		}
	}
	if latestUnconsumed != nil {
		return *latestUnconsumed, nil
	}
	if latestAny != nil {
		return *latestAny, nil
	}
	return domain.LoginChallenge{}, domain.ErrChallengeNotFound
}

func (s *fakeChallengeStore) ActiveByEmail(_ context.Context, email string) (domain.LoginChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latestUnconsumed *domain.LoginChallenge
	var latestAny *domain.LoginChallenge
	for i := range s.challenges {
		c := s.challenges[i]
		if c.Email != email {
			continue
		}
		if latestAny == nil || !c.CreatedAt.Before(latestAny.CreatedAt) {
			cc := c
			latestAny = &cc
		}
		if !c.IsConsumed() && (latestUnconsumed == nil || !c.CreatedAt.Before(latestUnconsumed.CreatedAt)) {
			cc := c
			latestUnconsumed = &cc
		}
	}
	if latestUnconsumed != nil {
		return *latestUnconsumed, nil
	}
	if latestAny != nil {
		return *latestAny, nil
	}
	return domain.LoginChallenge{}, domain.ErrChallengeNotFound
}

func (s *fakeChallengeStore) CountByEmailSince(_ context.Context, email string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, c := range s.challenges {
		if c.Email == email && !c.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (s *fakeChallengeStore) CountByPhoneSince(_ context.Context, phone string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, c := range s.challenges {
		if c.Phone == phone && !c.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

type fakeUserStore struct {
	users map[string]domain.User
	// byPhoneErr, when set, makes ByPhone fail with this error so tests can
	// exercise error propagation.
	byPhoneErr error
	// log, when set, records "user:<id>" on Save so tests can assert the
	// cross-store save order.
	log *[]string
}

func newFakeUserStore() *fakeUserStore { return &fakeUserStore{users: map[string]domain.User{}} }

func (s *fakeUserStore) Save(_ context.Context, u domain.User) error {
	if s.log != nil {
		*s.log = append(*s.log, "user:"+u.ID)
	}
	s.users[u.ID] = u
	return nil
}

func (s *fakeUserStore) ByPhone(_ context.Context, phone string) (domain.User, error) {
	if s.byPhoneErr != nil {
		return domain.User{}, s.byPhoneErr
	}
	for _, u := range s.users {
		if u.Phone == phone {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrUserNotFound
}

func (s *fakeUserStore) ByEmail(_ context.Context, email string) (domain.User, error) {
	for _, u := range s.users {
		if u.Email == email {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrUserNotFound
}

type fakeWorkspaceStore struct {
	workspaces map[string]domain.PersonalWorkspace
	// log, when set, records "workspace:<id>" on Save so tests can assert the
	// cross-store save order.
	log *[]string
}

func newFakeWorkspaceStore() *fakeWorkspaceStore {
	return &fakeWorkspaceStore{workspaces: map[string]domain.PersonalWorkspace{}}
}

func (s *fakeWorkspaceStore) Save(_ context.Context, ws domain.PersonalWorkspace) error {
	if s.log != nil {
		*s.log = append(*s.log, "workspace:"+ws.ID)
	}
	s.workspaces[ws.ID] = ws
	return nil
}

type fakeSessionStore struct {
	sessions map[string]domain.Session
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]domain.Session{}}
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

type fakeChannelStore struct {
	channels map[string]domain.ContactChannel
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{channels: map[string]domain.ContactChannel{}}
}

func (s *fakeChannelStore) Save(_ context.Context, c domain.ContactChannel) error {
	for _, existing := range s.channels {
		if existing.UserID == c.UserID && existing.Kind == c.Kind && existing.Address == c.Address {
			return domain.ErrChannelExists
		}
	}
	s.channels[c.ID] = c
	return nil
}

func (s *fakeChannelStore) Update(_ context.Context, c domain.ContactChannel) error {
	s.channels[c.ID] = c
	return nil
}

func (s *fakeChannelStore) ByID(_ context.Context, workspaceID, userID, channelID string) (domain.ContactChannel, error) {
	if c, ok := s.channels[channelID]; ok && c.UserID == userID && c.WorkspaceID == workspaceID {
		return c, nil
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

type fakeOutbox struct {
	messages []ports.OutboxMessage
}

func (o *fakeOutbox) Write(_ context.Context, m ports.OutboxMessage) error {
	o.messages = append(o.messages, m)
	return nil
}

// idGenerator returns deterministic incrementing IDs.
type idGenerator struct{ counter int }

func (g *idGenerator) next() string {
	g.counter++
	return "id-" + strconv.Itoa(g.counter)
}
