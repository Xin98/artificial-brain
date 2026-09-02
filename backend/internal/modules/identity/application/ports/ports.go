package ports

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// UserStore persists users.
type UserStore interface {
	Save(ctx context.Context, user domain.User) error
	// Update replaces the mutable identifiers (phone/email) of an existing
	// user, keeping the at-most-one-of-each identifier invariants.
	Update(ctx context.Context, user domain.User) error
	ByPhone(ctx context.Context, phone string) (domain.User, error)
	ByEmail(ctx context.Context, email string) (domain.User, error)
}

// WorkspaceStore persists personal workspaces.
type WorkspaceStore interface {
	Save(ctx context.Context, workspace domain.PersonalWorkspace) error
}

// ChallengeStore persists login challenges.
type ChallengeStore interface {
	Save(ctx context.Context, challenge domain.LoginChallenge) error
	Update(ctx context.Context, challenge domain.LoginChallenge) error
	ActiveByPhone(ctx context.Context, phone string) (domain.LoginChallenge, error)
	ActiveByEmail(ctx context.Context, email string) (domain.LoginChallenge, error)
	CountByPhoneSince(ctx context.Context, phone string, since time.Time) (int, error)
	CountByEmailSince(ctx context.Context, email string, since time.Time) (int, error)
}

// SessionStore persists sessions.
type SessionStore interface {
	Save(ctx context.Context, session domain.Session) error
	Update(ctx context.Context, session domain.Session) error
	ByID(ctx context.Context, sessionID string) (domain.Session, error)
	ByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error)
}

// ChannelStore persists contact channels.
type ChannelStore interface {
	Save(ctx context.Context, channel domain.ContactChannel) error
	Update(ctx context.Context, channel domain.ContactChannel) error
	ByID(ctx context.Context, workspaceID, userID, channelID string) (domain.ContactChannel, error)
	ListByUser(ctx context.Context, workspaceID, userID string) ([]domain.ContactChannel, error)
}

// OutboxMessage is a fake-adapter record of an outbound SMS/email. Plaintext
// codes live only here (the dev outbox); all other code storage is hash-only.
type OutboxMessage struct {
	Address string
	Channel string
	Purpose string
	Code    string
}

// MessageOutbox writes outbound messages. In ITER-0002 it is a fake adapter
// that records into the dev outbox table.
type MessageOutbox interface {
	Write(ctx context.Context, message OutboxMessage) error
}
