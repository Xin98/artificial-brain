package ports

import (
	"context"

	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

// TodoGateway is the seam to Todo's public application surface (D6). The cmd
// composition adapts Todo's handlers to it; the shim also maps Todo's domain
// errors to Conversation-owned errors.
type TodoGateway interface {
	CreateTodo(ctx context.Context, request tododto.CreateTodoRequest) (tododto.Todo, error)
	ListTodos(ctx context.Context, workspaceID, ownerUserID string, filters tododto.ListFilters) ([]tododto.Todo, error)
	SearchCandidates(ctx context.Context, workspaceID, ownerUserID, keyword string) ([]tododto.Candidate, error)
	// GetTodo returns ErrTodoNotFound (conversation-owned) for missing and
	// already-deleted todos rather than Todo's domain errors. Pending and
	// completed todos are both returned so either can be deleted through the
	// confirmation flow.
	GetTodo(ctx context.Context, workspaceID, ownerUserID, todoID string) (tododto.Todo, error)
	DeleteTodo(ctx context.Context, request tododto.DeleteTodoRequest) (tododto.Todo, error)
}
