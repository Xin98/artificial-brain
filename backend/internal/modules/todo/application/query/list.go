// Package query implements the Todo application queries.
package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
)

// ListTodosHandler lists visible todos with combinable filters.
type ListTodosHandler struct {
	Store ports.TodoStore
	Now   func() time.Time
}

// Handle returns matching todos, capped at dto.MaxListLimit, deleted never
// included, each carrying its derived overdue flag.
func (h *ListTodosHandler) Handle(ctx context.Context, workspaceID, ownerUserID string, filters dto.ListFilters) ([]dto.Todo, error) {
	todos, err := h.Store.List(ctx, workspaceID, ownerUserID, filters, dto.MaxListLimit)
	if err != nil {
		return nil, err
	}
	now := h.Now()
	views := make([]dto.Todo, 0, len(todos))
	for _, todo := range todos {
		views = append(views, dto.FromDomain(todo, now))
	}
	return views, nil
}
