package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// ImportTodoHandler restores an imported todo exactly as recorded. It carries
// no UnitOfWork of its own — the insert joins the caller's ambient
// transaction — and no Planner: import never schedules reminders (D7).
type ImportTodoHandler struct {
	Store ports.TodoStore
	NewID func() string
	Now   func() time.Time
}

// Handle restores the todo exactly as recorded (no UoW of its own — it joins
// the caller's ambient transaction; no Planner — import never schedules, D7).
func (h *ImportTodoHandler) Handle(ctx context.Context, request dto.ImportTodoRequest) (dto.Todo, error) {
	todo, err := domain.Restore(h.NewID(), request.WorkspaceID, request.UserID, request.Title,
		request.Description, request.DueAtUTC, request.TimezoneAtInput, domain.Status(request.Status),
		request.ReminderVersion, request.Version, request.CreatedAt, request.UpdatedAt,
		request.CompletedAt, request.DeletedAt)
	if err != nil {
		return dto.Todo{}, err
	}
	if err := h.Store.Insert(ctx, todo); err != nil {
		return dto.Todo{}, err
	}
	return dto.FromDomain(todo, h.Now()), nil
}
