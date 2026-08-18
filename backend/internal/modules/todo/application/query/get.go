package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
)

// GetTodoHandler fetches one todo scoped to the caller.
type GetTodoHandler struct {
	Store ports.TodoStore
	Now   func() time.Time
}

// Handle returns the todo view or domain.ErrTodoNotFound.
func (h *GetTodoHandler) Handle(ctx context.Context, workspaceID, ownerUserID, todoID string) (dto.Todo, error) {
	todo, err := h.Store.Get(ctx, workspaceID, ownerUserID, todoID)
	if err != nil {
		return dto.Todo{}, err
	}
	return dto.FromDomain(todo, h.Now()), nil
}
