package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// DeleteTodoHandler soft-deletes a todo and revokes its reminder plans
// inside one unit of work. It is the single delete entry point; both manual
// and smart deletes reach it through a Confirmation Request.
type DeleteTodoHandler struct {
	Store   ports.TodoStore
	UoW     ports.UnitOfWork
	Planner ports.ReminderPlanner
	Now     func() time.Time
}

// Handle soft-deletes the todo identified by request.
func (h *DeleteTodoHandler) Handle(ctx context.Context, request dto.DeleteTodoRequest) (dto.Todo, error) {
	var todo domain.Todo
	now := h.Now()
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		loaded, err := h.Store.Get(ctx, request.WorkspaceID, request.UserID, request.TodoID)
		if err != nil {
			return err
		}
		if err := loaded.Delete(request.Version, now); err != nil {
			return err
		}
		if err := h.Store.Update(ctx, loaded, request.Version); err != nil {
			return err
		}
		todo = loaded
		return h.Planner.Revoke(ctx, ports.RevokeReminderRequest{
			WorkspaceID:         request.WorkspaceID,
			TodoID:              request.TodoID,
			UpToReminderVersion: loaded.ReminderVersion,
		})
	})
	if err != nil {
		return dto.Todo{}, err
	}
	return dto.FromDomain(todo, now), nil
}
