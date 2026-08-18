package dto

import (
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// Todo is the application view of a todo, ready for JSON serialization.
type Todo struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Description     *string    `json:"description,omitempty"`
	DueAtUTC        *time.Time `json:"dueAtUtc,omitempty"`
	TimezoneAtInput *string    `json:"timezoneAtInput,omitempty"`
	Status          string     `json:"status"`
	Overdue         bool       `json:"overdue"`
	ReminderVersion int        `json:"reminderVersion"`
	Version         int        `json:"version"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

// FromDomain maps the aggregate to its view, deriving overdue at now.
func FromDomain(todo domain.Todo, now time.Time) Todo {
	return Todo{
		ID:              todo.ID,
		Title:           todo.Title,
		Description:     todo.Description,
		DueAtUTC:        todo.DueAtUTC,
		TimezoneAtInput: todo.TimezoneAtInput,
		Status:          string(todo.Status),
		Overdue:         todo.IsOverdue(now),
		ReminderVersion: todo.ReminderVersion,
		Version:         todo.Version,
		CreatedAt:       todo.CreatedAt,
		UpdatedAt:       todo.UpdatedAt,
		CompletedAt:     todo.CompletedAt,
		DeletedAt:       todo.DeletedAt,
	}
}

// CreateTodoRequest carries the fields for creating a todo.
type CreateTodoRequest struct {
	WorkspaceID     string
	UserID          string
	Title           string
	Description     *string
	DueAtUTC        *time.Time
	TimezoneAtInput *string
}

// CompleteTodoRequest carries a completion with its optimistic version.
type CompleteTodoRequest struct {
	WorkspaceID string
	UserID      string
	TodoID      string
	Version     int
}

// DeleteTodoRequest carries a soft delete with its optimistic version.
type DeleteTodoRequest struct {
	WorkspaceID string
	UserID      string
	TodoID      string
	Version     int
}

// UpdateTodoRequest carries a partial edit. DueChanged distinguishes an
// absent field from an explicit clear.
type UpdateTodoRequest struct {
	WorkspaceID     string
	UserID          string
	TodoID          string
	Version         int
	Title           *string
	Description     *string
	TimezoneAtInput *string
	DueChanged      bool
	DueAtUTC        *time.Time
}
