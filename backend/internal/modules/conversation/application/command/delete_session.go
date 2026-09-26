package command

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

// DeleteSessionHandler removes a session under the caller's workspace+user
// scope; the persisted transcript follows through the schema's ON DELETE
// CASCADE. A miss yields domain.ErrSessionNotFound.
type DeleteSessionHandler struct {
	Sessions ports.SessionStore
}

// Handle deletes the session.
func (h *DeleteSessionHandler) Handle(ctx context.Context, workspaceID, userID, sessionID string) error {
	return h.Sessions.Delete(ctx, workspaceID, userID, sessionID)
}
