package query

import (
	"context"
	"strconv"

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
	return h.HandlePage(ctx, workspaceID, userID, sessionID, "")
}

// HandlePage returns the latest page strictly older than before. The extra
// oldest row is the hasMore probe, so the visible boundary is never dropped.
func (h *GetHistoryHandler) HandlePage(ctx context.Context, workspaceID, userID, sessionID, before string) (dto.SessionHistoryView, error) {
	var boundary int64
	if before != "" {
		var err error
		boundary, err = strconv.ParseInt(before, 10, 64)
		if err != nil || boundary <= 0 {
			return dto.SessionHistoryView{}, ErrInvalidPage
		}
		for _, r := range before {
			if r < '0' || r > '9' {
				return dto.SessionHistoryView{}, ErrInvalidPage
			}
		}
	}
	session, err := h.Sessions.Get(ctx, workspaceID, userID, sessionID)
	if err != nil {
		return dto.SessionHistoryView{}, err
	}
	var entries []ports.MessageLogEntry
	if paged, ok := h.Messages.(ports.PagedMessageLogStore); ok {
		entries, err = paged.ListBefore(ctx, workspaceID, userID, sessionID, before, MaxHistoryMessages+1)
	} else {
		limit := MaxHistoryMessages + 1
		if before != "" {
			limit = 2147483647
		}
		entries, err = h.Messages.ListBySession(ctx, workspaceID, userID, sessionID, limit)
		if before != "" {
			filtered := make([]ports.MessageLogEntry, 0, len(entries))
			for _, entry := range entries {
				id, parseErr := strconv.ParseInt(entry.ID, 10, 64)
				if parseErr == nil && id < boundary {
					filtered = append(filtered, entry)
				}
			}
			entries = filtered
		}
	}
	if err != nil {
		return dto.SessionHistoryView{}, err
	}
	hasMore := len(entries) > MaxHistoryMessages
	if hasMore {
		entries = entries[len(entries)-MaxHistoryMessages:]
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
	view := dto.SessionHistoryView{
		SessionID: session.ID,
		Title:     session.Title,
		Messages:  messages,
		HasMore:   hasMore,
	}
	if hasMore {
		view.NextBefore = messages[0].ID
	}
	return view, nil
}
