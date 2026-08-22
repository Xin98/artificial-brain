package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// ExportTodosHandler pages the owner's full-history todos as export records;
// deleted and completed todos are included.
type ExportTodosHandler struct {
	Store ports.TodoStore
	Now   func() time.Time
}

// Handle pages the owner's full-history todos (offset, limit capped at 200)
// as export records; deleted and completed todos included.
func (h *ExportTodosHandler) Handle(ctx context.Context, workspaceID, ownerUserID string, offset, limit int) ([]dto.TodoExportRecord, error) {
	if limit > dto.MaxListLimit {
		limit = dto.MaxListLimit
	}
	todos, err := h.Store.ListAll(ctx, workspaceID, ownerUserID, offset, limit)
	if err != nil {
		return nil, err
	}
	records := make([]dto.TodoExportRecord, 0, len(todos))
	for _, todo := range todos {
		records = append(records, toExportRecord(todo))
	}
	return records, nil
}

// toExportRecord maps the aggregate to its export row, preserving the
// historical timestamps and dropping nothing but the optimistic version.
func toExportRecord(todo domain.Todo) dto.TodoExportRecord {
	return dto.TodoExportRecord{
		ID:              todo.ID,
		Title:           todo.Title,
		Description:     todo.Description,
		DueAtUTC:        todo.DueAtUTC,
		TimezoneAtInput: todo.TimezoneAtInput,
		Status:          string(todo.Status),
		ReminderVersion: todo.ReminderVersion,
		CreatedAt:       todo.CreatedAt,
		UpdatedAt:       todo.UpdatedAt,
		CompletedAt:     todo.CompletedAt,
		DeletedAt:       todo.DeletedAt,
	}
}
