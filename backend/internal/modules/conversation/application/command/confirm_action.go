package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

// ConfirmActionHandler consumes a confirmation and executes the bound delete
// in one unit of work: conditional consume, todo version re-check, then the
// gateway delete.
type ConfirmActionHandler struct {
	Confirmations ports.ConfirmationStore
	Todos         ports.TodoGateway
	UoW           ports.UnitOfWork
	Now           func() time.Time
}

// Handle confirms and executes the bound action.
func (h *ConfirmActionHandler) Handle(ctx context.Context, workspaceID, userID, confirmationID string) (dto.MessageResponse, error) {
	var todoID string
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		confirmation, err := h.Confirmations.Get(ctx, workspaceID, userID, confirmationID)
		if err != nil {
			return err
		}
		if err := h.Confirmations.Consume(ctx, workspaceID, userID, confirmationID, h.Now()); err != nil {
			return err
		}
		todo, err := h.Todos.GetTodo(ctx, workspaceID, userID, confirmation.TodoID)
		if err != nil {
			return err
		}
		if todo.Version != confirmation.TodoVersion {
			return domain.ErrConfirmationTodoVersionStale
		}
		if _, err := h.Todos.DeleteTodo(ctx, tododto.DeleteTodoRequest{
			WorkspaceID: workspaceID,
			UserID:      userID,
			TodoID:      confirmation.TodoID,
			Version:     todo.Version,
		}); err != nil {
			return err
		}
		todoID = confirmation.TodoID
		return nil
	})
	if err != nil {
		return dto.MessageResponse{}, err
	}
	return dto.MessageResponse{Kind: dto.KindTodoDeleted, TodoID: todoID}, nil
}
