package command

import (
	"context"
	"strings"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// CreateSessionHandler creates an empty session from the sidebar. An absent
// title falls back to domain.DefaultSessionTitleFallback; an out-of-bounds
// title yields domain.ErrSessionTitleInvalid.
type CreateSessionHandler struct {
	Sessions ports.SessionStore
	NewID    func() string
	Now      func() time.Time
}

// Handle validates and persists the new session.
func (h *CreateSessionHandler) Handle(ctx context.Context, workspaceID, userID, title string) (dto.SessionView, error) {
	if strings.TrimSpace(title) == "" {
		title = domain.DefaultSessionTitleFallback
	}
	session, err := domain.NewSession(h.NewID(), workspaceID, userID, title, h.Now())
	if err != nil {
		return dto.SessionView{}, err
	}
	if err := h.Sessions.Create(ctx, session); err != nil {
		return dto.SessionView{}, err
	}
	return application.SessionView(session), nil
}
