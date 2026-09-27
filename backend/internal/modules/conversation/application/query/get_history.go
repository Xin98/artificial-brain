package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

// MaxHistoryMessages bounds one session history response; rows are the
// latest N in ascending insertion order.
const MaxHistoryMessages = 200

// GetHistoryHandler replays one session's transcript as text. A miss yields
// domain.ErrSessionNotFound.
type GetHistoryHandler struct {
	Sessions ports.SessionStore
	Messages ports.MessageLogStore
}

// Handle loads the session and its persisted transcript.
func (h *GetHistoryHandler) Handle(ctx context.Context, workspaceID, userID, sessionID string) (dto.SessionHistoryView, error) {
	session, err := h.Sessions.Get(ctx, workspaceID, userID, sessionID)
	if err != nil {
		return dto.SessionHistoryView{}, err
	}
	entries, err := h.Messages.ListBySession(ctx, workspaceID, userID, sessionID, MaxHistoryMessages)
	if err != nil {
		return dto.SessionHistoryView{}, err
	}
	messages := make([]dto.MessageView, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, dto.MessageView{
			ID:             entry.ID,
			Role:           entry.Role,
			Body:           entry.Body,
			ResolvedIntent: entry.ResolvedIntent,
			CreatedAt:      entry.CreatedAt,
		})
	}
	return dto.SessionHistoryView{
		SessionID: session.ID,
		Title:     session.Title,
		Messages:  messages,
	}, nil
}
