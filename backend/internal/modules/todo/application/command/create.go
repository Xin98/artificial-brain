// Package command implements the Todo application commands. Every handler
// composes store writes and reminder seam calls inside the caller-provided
// UnitOfWork and never begins transactions itself.
package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// CreateTodoHandler inserts a todo and, when it carries a due instant,
// plans its first reminder inside the same unit of work.
type CreateTodoHandler struct {
	Store    ports.TodoStore
	UoW      ports.UnitOfWork
	Planner  ports.ReminderPlanner
	Channels ports.ChannelsProvider
	NewID    func() string
	Now      func() time.Time
}

// Handle creates the todo described by request.
func (h *CreateTodoHandler) Handle(ctx context.Context, request dto.CreateTodoRequest) (dto.Todo, error) {
	now := h.Now()
	todo, err := domain.New(h.NewID(), request.WorkspaceID, request.UserID, request.Title,
		request.Description, request.DueAtUTC, request.TimezoneAtInput, now)
	if err != nil {
		return dto.Todo{}, err
	}
	err = h.UoW.Run(ctx, func(ctx context.Context) error {
		if err := h.Store.Insert(ctx, todo); err != nil {
			return err
		}
		if todo.DueAtUTC == nil {
			return nil
		}
		channels, err := channelsSnapshot(ctx, h.Channels, todo.WorkspaceID, todo.OwnerUserID)
		if err != nil {
			return err
		}
		return h.Planner.Plan(ctx, ports.PlanReminderRequest{
			WorkspaceID:         todo.WorkspaceID,
			TodoID:              todo.ID,
			TodoReminderVersion: todo.ReminderVersion,
			ScheduledAtUTC:      *todo.DueAtUTC,
			Channels:            channels,
		})
	})
	if err != nil {
		return dto.Todo{}, err
	}
	return dto.FromDomain(todo, now), nil
}

// channelsSnapshot resolves the owner's channel snapshot, treating a nil
// provider as an empty snapshot.
func channelsSnapshot(ctx context.Context, provider ports.ChannelsProvider, workspaceID, ownerUserID string) ([]string, error) {
	if provider == nil {
		return []string{}, nil
	}
	return provider(ctx, workspaceID, ownerUserID)
}
