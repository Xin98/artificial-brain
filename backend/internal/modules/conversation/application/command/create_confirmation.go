package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// CreateConfirmationHandler is the single manual-delete entry point: it
// validates the todo through the gateway and binds a short-lived,
// single-use confirmation to it.
type CreateConfirmationHandler struct {
	Todos           ports.TodoGateway
	Confirmations   ports.ConfirmationStore
	NewID           func() string
	Now             func() time.Time
	ConfirmationTTL time.Duration
}

// Handle creates the confirmation for a concrete todo.
func (h *CreateConfirmationHandler) Handle(ctx context.Context, workspaceID, userID, intent, todoID string) (domain.ConfirmationRequest, error) {
	typed := domain.Intent(intent)
	if typed != domain.IntentTodoDelete {
		return domain.ConfirmationRequest{}, domain.ErrUnsupportedConfirmationIntent
	}
	todo, err := h.Todos.GetTodo(ctx, workspaceID, userID, todoID)
	if err != nil {
		return domain.ConfirmationRequest{}, err
	}
	confirmation, err := domain.NewConfirmationRequest(h.NewID(), workspaceID, userID, typed, todoID, todo.Version, h.Now(), h.ConfirmationTTL)
	if err != nil {
		return domain.ConfirmationRequest{}, err
	}
	if err := h.Confirmations.Save(ctx, confirmation); err != nil {
		return domain.ConfirmationRequest{}, err
	}
	return confirmation, nil
}
