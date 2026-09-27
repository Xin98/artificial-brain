// Package query implements the Conversation read models.
package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

// MaxListedSessions bounds the sidebar listing (v1 has no pagination).
const MaxListedSessions = 100

// ListSessionsHandler returns the caller's sessions, most recently active
// first.
type ListSessionsHandler struct {
	Sessions ports.SessionStore
}

// Handle lists the scoped sessions.
func (h *ListSessionsHandler) Handle(ctx context.Context, workspaceID, userID string) (dto.SessionListView, error) {
	sessions, err := h.Sessions.List(ctx, workspaceID, userID, MaxListedSessions)
	if err != nil {
		return dto.SessionListView{}, err
	}
	views := make([]dto.SessionView, 0, len(sessions))
	for _, session := range sessions {
		views = append(views, application.SessionView(session))
	}
	return dto.SessionListView{Sessions: views}, nil
}
