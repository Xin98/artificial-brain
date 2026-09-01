package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// MaxChallengesPerPhonePerHour bounds how many login codes a single
// identifier (phone or email) may request in a rolling hour. The name is
// kept deliberately: it bounded phones historically and now bounds any one
// identifier.
const MaxChallengesPerPhonePerHour = 5

// validateIdentifier enforces that exactly one of phone or email is present
// and well-formed, returning the normalized identifier.
func validateIdentifier(identifier domain.LoginIdentifier) (domain.LoginIdentifier, error) {
	return domain.NewLoginIdentifier(identifier.Phone, identifier.Email)
}

// RequestLoginChallengeHandler creates a login challenge and sends its code
// via the outbound message port.
type RequestLoginChallengeHandler struct {
	Challenges   ports.ChallengeStore
	Outbox       ports.MessageOutbox
	NewCode      func() (string, error)
	NewID        func() string
	Now          func() time.Time
	ChallengeTTL time.Duration
	// PrivateAdminPhone and PrivateAdminEmail restrict login to the fixed
	// private-deployment admin identifiers; any other identifier is rejected
	// before any store or outbox interaction. Both empty keeps public-cloud
	// behavior.
	PrivateAdminPhone string
	PrivateAdminEmail string
}

func (h *RequestLoginChallengeHandler) Handle(ctx context.Context, identifier domain.LoginIdentifier) error {
	identifier, err := validateIdentifier(identifier)
	if err != nil {
		return err
	}
	if h.privateBlocked(identifier) {
		return domain.ErrRegistrationClosed
	}
	now := h.Now()
	count, err := h.countRecent(ctx, identifier, now)
	if err != nil {
		return err
	}
	if count >= MaxChallengesPerPhonePerHour {
		return domain.ErrRateLimited
	}
	code, err := h.NewCode()
	if err != nil {
		return err
	}
	challenge := domain.LoginChallenge{
		ID:        h.NewID(),
		Phone:     identifier.Phone,
		Email:     identifier.Email,
		CodeHash:  domain.HashCode(code),
		CreatedAt: now,
		ExpiresAt: now.Add(h.ChallengeTTL),
	}
	if err := h.Challenges.Save(ctx, challenge); err != nil {
		return err
	}
	return h.Outbox.Write(ctx, ports.OutboxMessage{
		Address: identifier.Value(),
		Channel: identifier.Channel(),
		Purpose: "login",
		Code:    code,
	})
}

func (h *RequestLoginChallengeHandler) privateBlocked(identifier domain.LoginIdentifier) bool {
	if h.PrivateAdminPhone == "" && h.PrivateAdminEmail == "" {
		return false
	}
	if identifier.Phone != "" && identifier.Phone == h.PrivateAdminPhone {
		return false
	}
	if identifier.Email != "" && identifier.Email == h.PrivateAdminEmail {
		return false
	}
	return true
}

func (h *RequestLoginChallengeHandler) countRecent(ctx context.Context, identifier domain.LoginIdentifier, now time.Time) (int, error) {
	if identifier.Phone != "" {
		return h.Challenges.CountByPhoneSince(ctx, identifier.Phone, now.Add(-time.Hour))
	}
	return h.Challenges.CountByEmailSince(ctx, identifier.Email, now.Add(-time.Hour))
}

// VerifyLoginChallengeHandler validates a login code, registers the user and
// workspace on first login, and issues a session.
type VerifyLoginChallengeHandler struct {
	Challenges ports.ChallengeStore
	Users      ports.UserStore
	Workspaces ports.WorkspaceStore
	Sessions   ports.SessionStore
	NewID      func() string
	NewToken   func() (string, error)
	Now        func() time.Time
	SessionTTL time.Duration
	// PrivateAdminPhone and PrivateAdminEmail restrict login to the fixed
	// private-deployment admin identifiers; any other identifier is rejected
	// before any store interaction. Both empty keeps public-cloud behavior.
	PrivateAdminPhone string
	PrivateAdminEmail string
}

func (h *VerifyLoginChallengeHandler) Handle(ctx context.Context, identifier domain.LoginIdentifier, code string) (dto.VerifyLoginChallengeResult, error) {
	identifier, err := validateIdentifier(identifier)
	if err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	if h.privateBlocked(identifier) {
		return dto.VerifyLoginChallengeResult{}, domain.ErrRegistrationClosed
	}
	if _, err := domain.NewCode(code); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	challenge, err := h.activeChallenge(ctx, identifier)
	if err != nil {
		return dto.VerifyLoginChallengeResult{}, domain.ErrChallengeNotFound
	}
	now := h.Now()
	if challenge.IsExpired(now) {
		return dto.VerifyLoginChallengeResult{}, domain.ErrChallengeExpired
	}
	if challenge.IsConsumed() {
		return dto.VerifyLoginChallengeResult{}, domain.ErrChallengeConsumed
	}
	if challenge.Attempts >= domain.MaxVerifyAttempts {
		return dto.VerifyLoginChallengeResult{}, domain.ErrTooManyAttempts
	}
	if !challenge.Matches(domain.HashCode(code)) {
		exhausted := challenge.RegisterFailedAttempt()
		if updateErr := h.Challenges.Update(ctx, challenge); updateErr != nil {
			return dto.VerifyLoginChallengeResult{}, updateErr
		}
		if exhausted {
			return dto.VerifyLoginChallengeResult{}, domain.ErrTooManyAttempts
		}
		return dto.VerifyLoginChallengeResult{}, domain.ErrInvalidCode
	}
	if err := challenge.Consume(now); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	if err := h.Challenges.Update(ctx, challenge); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	user, err := h.existingUser(ctx, identifier)
	if errors.Is(err, domain.ErrUserNotFound) {
		workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
		if err := h.Workspaces.Save(ctx, workspace); err != nil {
			return dto.VerifyLoginChallengeResult{}, err
		}
		user = domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: identifier.Phone, Email: identifier.Email, CreatedAt: now}
		if err := h.Users.Save(ctx, user); err != nil {
			return dto.VerifyLoginChallengeResult{}, err
		}
	} else if err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	token, err := h.NewToken()
	if err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	session := domain.Session{
		ID:          h.NewID(),
		UserID:      user.ID,
		WorkspaceID: user.WorkspaceID,
		TokenHash:   domain.HashCode(token),
		CreatedAt:   now,
		ExpiresAt:   now.Add(h.SessionTTL),
	}
	if err := h.Sessions.Save(ctx, session); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	return dto.VerifyLoginChallengeResult{
		Token: token,
		Principal: dto.Principal{
			UserID:      user.ID,
			WorkspaceID: user.WorkspaceID,
			SessionID:   session.ID,
		},
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (h *VerifyLoginChallengeHandler) privateBlocked(identifier domain.LoginIdentifier) bool {
	if h.PrivateAdminPhone == "" && h.PrivateAdminEmail == "" {
		return false
	}
	if identifier.Phone != "" && identifier.Phone == h.PrivateAdminPhone {
		return false
	}
	if identifier.Email != "" && identifier.Email == h.PrivateAdminEmail {
		return false
	}
	return true
}

func (h *VerifyLoginChallengeHandler) activeChallenge(ctx context.Context, identifier domain.LoginIdentifier) (domain.LoginChallenge, error) {
	if identifier.Phone != "" {
		return h.Challenges.ActiveByPhone(ctx, identifier.Phone)
	}
	return h.Challenges.ActiveByEmail(ctx, identifier.Email)
}

func (h *VerifyLoginChallengeHandler) existingUser(ctx context.Context, identifier domain.LoginIdentifier) (domain.User, error) {
	if identifier.Phone != "" {
		return h.Users.ByPhone(ctx, identifier.Phone)
	}
	return h.Users.ByEmail(ctx, identifier.Email)
}

// LogoutHandler revokes a session.
type LogoutHandler struct {
	Sessions ports.SessionStore
	Now      func() time.Time
}

func (h *LogoutHandler) Handle(ctx context.Context, sessionID string) error {
	session, err := h.Sessions.ByID(ctx, sessionID)
	if err != nil {
		return domain.ErrSessionNotFound
	}
	session.Revoke(h.Now())
	return h.Sessions.Update(ctx, session)
}
