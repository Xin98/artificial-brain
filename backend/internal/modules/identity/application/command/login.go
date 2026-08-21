package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// MaxChallengesPerPhonePerHour bounds how many login codes a phone may request
// in a rolling hour.
const MaxChallengesPerPhonePerHour = 5

// RequestLoginChallengeHandler creates a login challenge and sends its code via
// the outbound message port.
type RequestLoginChallengeHandler struct {
	Challenges   ports.ChallengeStore
	Outbox       ports.MessageOutbox
	NewCode      func() (string, error)
	NewID        func() string
	Now          func() time.Time
	ChallengeTTL time.Duration
	// PrivateAdminPhone, when non-empty, restricts login to that single
	// private-deployment admin phone; every other phone is rejected before
	// any store or outbox interaction. Empty keeps public-cloud behavior.
	PrivateAdminPhone string
}

func (h *RequestLoginChallengeHandler) Handle(ctx context.Context, phone string) error {
	p, err := domain.NewPhone(phone)
	if err != nil {
		return err
	}
	if h.PrivateAdminPhone != "" && p.String() != h.PrivateAdminPhone {
		return domain.ErrRegistrationClosed
	}
	now := h.Now()
	count, err := h.Challenges.CountByPhoneSince(ctx, p.String(), now.Add(-time.Hour))
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
		Phone:     p.String(),
		CodeHash:  domain.HashCode(code),
		CreatedAt: now,
		ExpiresAt: now.Add(h.ChallengeTTL),
	}
	if err := h.Challenges.Save(ctx, challenge); err != nil {
		return err
	}
	return h.Outbox.Write(ctx, ports.OutboxMessage{
		Address: p.String(),
		Channel: "sms",
		Purpose: "login",
		Code:    code,
	})
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
	// PrivateAdminPhone, when non-empty, restricts login to that single
	// private-deployment admin phone; every other phone is rejected before
	// any store interaction. Empty keeps public-cloud behavior.
	PrivateAdminPhone string
}

func (h *VerifyLoginChallengeHandler) Handle(ctx context.Context, phone, code string) (dto.VerifyLoginChallengeResult, error) {
	p, err := domain.NewPhone(phone)
	if err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	if h.PrivateAdminPhone != "" && p.String() != h.PrivateAdminPhone {
		return dto.VerifyLoginChallengeResult{}, domain.ErrRegistrationClosed
	}
	if _, err := domain.NewCode(code); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	challenge, err := h.Challenges.ActiveByPhone(ctx, p.String())
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

	user, err := h.Users.ByPhone(ctx, p.String())
	if errors.Is(err, domain.ErrUserNotFound) {
		workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
		if err := h.Workspaces.Save(ctx, workspace); err != nil {
			return dto.VerifyLoginChallengeResult{}, err
		}
		user = domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: p.String(), CreatedAt: now}
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
