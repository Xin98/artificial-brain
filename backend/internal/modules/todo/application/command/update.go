package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// UpdateTodoHandler applies a partial edit. When the due instant changes it
// revokes the plans of the old reminder version and plans the new one in
// the same unit of work.
type UpdateTodoHandler struct {
	Store    ports.TodoStore
	UoW      ports.UnitOfWork
	Planner  ports.ReminderPlanner
	Channels ports.ChannelsProvider
	Now      func() time.Time
}

// Handle applies the edit described by request.
func (h *UpdateTodoHandler) Handle(ctx context.Context, request dto.UpdateTodoRequest) (dto.Todo, error) {
	var todo domain.Todo
	now := h.Now()
	err := h.UoW.Run(ctx, func(ctx context.Context) error {
		loaded, err := h.Store.Get(ctx, request.WorkspaceID, request.UserID, request.TodoID)
		if err != nil {
			return err
		}
		previousReminderVersion := loaded.ReminderVersion
		changes := domain.UpdateChanges{
			Title:           request.Title,
			Description:     request.Description,
			TimezoneAtInput: request.TimezoneAtInput,
			DueChanged:      request.DueChanged,
			DueAtUTC:        request.DueAtUTC,
		}
		if err := loaded.Update(request.Version, changes, now); err != nil {
			return err
		}
		if err := h.Store.Update(ctx, loaded, request.Version); err != nil {
			return err
		}
		todo = loaded
		if loaded.ReminderVersion == previousReminderVersion {
			return nil
		}
		if err := h.Planner.Revoke(ctx, ports.RevokeReminderRequest{
			WorkspaceID:         request.WorkspaceID,
			TodoID:              request.TodoID,
			UpToReminderVersion: previousReminderVersion,
		}); err != nil {
			return err
		}
		if loaded.DueAtUTC == nil {
			return nil
		}
		channels, err := channelsSnapshot(ctx, h.Channels, loaded.WorkspaceID, loaded.OwnerUserID)
		if err != nil {
			return err
		}
		return h.Planner.Plan(ctx, ports.PlanReminderRequest{
			WorkspaceID:         loaded.WorkspaceID,
			TodoID:              loaded.ID,
			TodoReminderVersion: loaded.ReminderVersion,
			ScheduledAtUTC:      *loaded.DueAtUTC,
			Channels:            channels,
			Title:               loaded.Title,
			OwnerUserID:         loaded.OwnerUserID,
		})
	})
	if err != nil {
		return dto.Todo{}, err
	}
	return dto.FromDomain(todo, now), nil
}
