// Package query implements the Conversation read models.
package query

import (
	"context"
	"errors"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// MaxListedSessions bounds a sidebar page.
const MaxListedSessions = 100

var ErrInvalidPage = errors.New("invalid conversation page")

// ListSessionsHandler returns the caller's sessions, most recently active
// first.
type ListSessionsHandler struct {
	Sessions ports.SessionStore
}

// Handle lists the scoped sessions.
func (h *ListSessionsHandler) Handle(ctx context.Context, workspaceID, userID string) (dto.SessionListView, error) {
	return h.HandlePage(ctx, workspaceID, userID, 0)
}

// HandlePage lists a bounded page and probes one extra row for hasMore.
func (h *ListSessionsHandler) HandlePage(ctx context.Context, workspaceID, userID string, offset int) (dto.SessionListView, error) {
	if offset < 0 || offset > 2147483647-MaxListedSessions-1 {
		return dto.SessionListView{}, ErrInvalidPage
	}
	var sessions []domain.Session
	var err error
	if paged, ok := h.Sessions.(ports.PagedSessionStore); ok {
		sessions, err = paged.ListPage(ctx, workspaceID, userID, offset, MaxListedSessions+1)
	} else {
		sessions, err = h.Sessions.List(ctx, workspaceID, userID, offset+MaxListedSessions+1)
		if offset >= len(sessions) {
			sessions = nil
		} else {
			sessions = sessions[offset:]
		}
	}
	if err != nil {
		return dto.SessionListView{}, err
	}
	hasMore := len(sessions) > MaxListedSessions
	if hasMore {
		sessions = sessions[:MaxListedSessions]
	}
	views := make([]dto.SessionView, 0, len(sessions))
	for _, session := range sessions {
		views = append(views, application.SessionView(session))
	}
	view := dto.SessionListView{Sessions: views, HasMore: hasMore}
	if hasMore {
		next := offset + len(views)
		view.NextOffset = &next
	}
	return view, nil
}
