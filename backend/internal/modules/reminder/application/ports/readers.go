package ports

import (
	"context"
	"errors"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
)

// ErrTodoNotFound reports that the todo a plan points at no longer exists.
// The cmd-local TodoReader shim maps the todo module's own not-found error to
// this sentinel so the reminder application layer never imports the todo
// module.
var ErrTodoNotFound = errors.New("reminder: todo not found")

// TodoReader re-reads the todo a plan points at, execution time, so the send
// command can suppress deliveries whose todo was completed, deleted, or
// rescheduled after planning. Get mirrors the todo module's scoped store Get
// (workspace + owner + todo) so the cmd shim is a direct delegation.
type TodoReader interface {
	Get(ctx context.Context, workspaceID, ownerUserID, todoID string) (dto.TodoView, error)
}

// ChannelResolver re-reads the contact endpoint for one user and channel,
// execution time. A missing channel is not an error: it resolves to a zero,
// unusable endpoint with a nil error so the send command suppresses.
type ChannelResolver interface {
	Resolve(ctx context.Context, workspaceID, userID, channel string) (dto.ChannelEndpoint, error)
}
