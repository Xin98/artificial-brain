package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// PlanReminderHandler persists a reminder plan and enqueues its job. It owns
// no transaction: the caller (Todo's unit of work) provides the ambient
// transaction, so a scheduler failure rolls back plan and todo together.
// A duplicate (todo_id, todo_reminder_version) is idempotent success and
// skips scheduling, because the original attempt already scheduled.
type PlanReminderHandler struct {
	Plans     ports.PlanStore
	Scheduler ports.JobScheduler
	NewID     func() string
	Now       func() time.Time
}

// Handle plans the reminder described by request.
func (h *PlanReminderHandler) Handle(ctx context.Context, request dto.PlanRequest) error {
	plan, err := domain.NewReminderPlan(h.NewID(), request.WorkspaceID, request.TodoID,
		request.TodoReminderVersion, request.ScheduledAtUTC, request.Channels, h.Now())
	if err != nil {
		return err
	}
	if err := h.Plans.Save(ctx, plan); err != nil {
		if errors.Is(err, domain.ErrPlanExists) {
			return nil
		}
		return err
	}
	return h.Scheduler.Schedule(ctx, ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         plan.WorkspaceID,
		TodoID:              plan.TodoID,
		TodoReminderVersion: plan.TodoReminderVersion,
		ScheduledAtUTC:      plan.ScheduledAtUTC,
		Channels:            plan.RequestedChannels,
	})
}
