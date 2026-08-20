package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// PlanReminderHandler persists a reminder plan with one delivery row per
// requested channel and enqueues its jobs. It owns no transaction: the caller
// (Todo's unit of work) provides the ambient transaction, so a scheduler
// failure rolls back plan, deliveries, and todo together. A duplicate
// (todo_id, todo_reminder_version) is idempotent success and skips deliveries
// and scheduling, because the original attempt already scheduled.
type PlanReminderHandler struct {
	Plans      ports.PlanStore
	Deliveries ports.DeliveryStore
	Scheduler  ports.JobScheduler
	NewID      func() string
	Now        func() time.Time
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
	deliveryIDByChannel := make(map[string]string, len(plan.RequestedChannels))
	for _, channel := range plan.RequestedChannels {
		delivery, err := domain.NewDelivery(h.NewID(), plan.WorkspaceID, request.OwnerUserID,
			plan.TodoID, plan.TodoReminderVersion, plan.ID, channel, request.Title,
			plan.ScheduledAtUTC, h.Now())
		if err != nil {
			return err
		}
		if err := h.Deliveries.Save(ctx, delivery); err != nil && !errors.Is(err, domain.ErrDeliveryExists) {
			return err
		}
		deliveryIDByChannel[channel] = delivery.ID
	}
	scheduled, err := h.Scheduler.Schedule(ctx, ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         plan.WorkspaceID,
		TodoID:              plan.TodoID,
		TodoReminderVersion: plan.TodoReminderVersion,
		ScheduledAtUTC:      plan.ScheduledAtUTC,
		Channels:            plan.RequestedChannels,
		OwnerUserID:         request.OwnerUserID,
	})
	if err != nil {
		return err
	}
	for _, scheduledChannel := range scheduled {
		deliveryID, ok := deliveryIDByChannel[scheduledChannel.Channel]
		if !ok {
			continue
		}
		if err := h.Deliveries.SetProviderJobID(ctx, plan.WorkspaceID, deliveryID, scheduledChannel.JobID); err != nil {
			return err
		}
	}
	return nil
}
