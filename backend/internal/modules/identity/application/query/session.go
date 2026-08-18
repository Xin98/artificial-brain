package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// SessionQuery resolves a bearer token to an authenticated principal.
type SessionQuery struct {
	Sessions ports.SessionStore
	Now      func() time.Time
}

func (q *SessionQuery) Authenticate(ctx context.Context, token string) (dto.Principal, error) {
	session, err := q.Sessions.ByTokenHash(ctx, domain.HashCode(token))
	if err != nil {
		return dto.Principal{}, domain.ErrSessionNotFound
	}
	if !session.IsActive(q.Now()) {
		return dto.Principal{}, domain.ErrSessionInactive
	}
	return dto.Principal{
		UserID:      session.UserID,
		WorkspaceID: session.WorkspaceID,
		SessionID:   session.ID,
	}, nil
}
