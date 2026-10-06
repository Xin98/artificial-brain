package command

import (
	"context"
	"fmt"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// ImportHistoryHandler restores passive rows only; it has no model, router,
// todo, reminder or confirmation ports, so history cannot execute operations.
type ImportHistoryHandler struct {
	Store ports.HistoryImporter
	NewID func() string
}

func (h *ImportHistoryHandler) ImportSession(ctx context.Context, workspace, user string, r dto.HistorySession) (string, error) {
	id := h.NewID()
	session, err := domain.NewSession(id, workspace, user, r.Title, r.CreatedAt)
	if err != nil {
		return "", err
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return "", domain.ErrInvalidSession
	}
	r.ID = session.ID
	r.Title = session.Title
	return id, h.Store.ImportSession(ctx, workspace, user, id, r)
}

func (h *ImportHistoryHandler) ImportMessage(ctx context.Context, workspace, user string, r dto.HistoryMessage) (string, error) {
	if workspace == "" || user == "" || r.CreatedAt.IsZero() || r.Order < 1 || (r.Role != ports.RoleUser && r.Role != ports.RoleAssistant) || (r.SessionID != nil && *r.SessionID == "") {
		return "", fmt.Errorf("%w: invalid history message", domain.ErrInvalidSession)
	}
	return h.Store.ImportMessage(ctx, workspace, user, r)
}
