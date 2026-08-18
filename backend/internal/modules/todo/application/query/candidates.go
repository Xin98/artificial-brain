package query

import (
	"context"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
)

// SearchCandidatesHandler searches pending todos for delete intents.
type SearchCandidatesHandler struct {
	Store ports.TodoStore
}

// Handle returns candidates capped at dto.MaxCandidateLimit, each carrying
// its current Version for later confirmation re-checks.
func (h *SearchCandidatesHandler) Handle(ctx context.Context, workspaceID, ownerUserID, keyword string) ([]dto.Candidate, error) {
	return h.Store.SearchCandidates(ctx, workspaceID, ownerUserID, keyword, dto.MaxCandidateLimit)
}
