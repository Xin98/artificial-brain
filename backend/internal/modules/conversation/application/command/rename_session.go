package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

// RenameSessionHandler retitles a session under the caller's workspace+user
// scope. A miss yields domain.ErrSessionNotFound; an out-of-bounds title
// yields domain.ErrSessionTitleInvalid.
type RenameSessionHandler struct {
	Sessions ports.SessionStore
	Now      func() time.Time
}

// Handle validates the new title against the loaded session and persists it.
func (h *RenameSessionHandler) Handle(ctx context.Context, workspaceID, userID, sessionID, title string) (dto.SessionView, error) {
	session, err := h.Sessions.Get(ctx, workspaceID, userID, sessionID)
	if err != nil {
		return dto.SessionView{}, err
	}
	renamed, err := session.Renamed(title, h.Now())
	if err != nil {
		return dto.SessionView{}, err
	}
	if err := h.Sessions.Rename(ctx, workspaceID, userID, sessionID, renamed.Title, renamed.UpdatedAt); err != nil {
		return dto.SessionView{}, err
	}
	return application.SessionView(renamed), nil
}
